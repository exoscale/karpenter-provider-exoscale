package userdata

import (
	"context"
	"fmt"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/providers/userdata/bootstrap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type Provider struct {
	kubeClient client.Client
}

func NewProvider(kubeClient client.Client) *Provider {
	return &Provider{kubeClient: kubeClient}
}

func (p *Provider) Generate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, clusterEndpoint, clusterDomain, bootstrapToken string) (string, error) {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithValues(
		"provider", "userdata",
		"nodeClass", nodeClass.Name,
		"nodeClaim", nodeClaim.Name,
	))

	caBundle, err := p.getClusterCA(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get cluster CA: %w", err)
	}

	registry, err := NewRegistryResolver(p.kubeClient).Resolve(ctx, nodeClass.Spec.ContainerRegistry)
	if err != nil {
		return "", fmt.Errorf("failed to resolve container registry: %w", err)
	}

	kubelet := nodeClass.Spec.Kubelet
	options := &bootstrap.Options{
		ClusterEndpoint:             clusterEndpoint,
		ClusterDNS:                  kubelet.ClusterDNS,
		ClusterDomain:               clusterDomain,
		BootstrapToken:              bootstrapToken,
		CABundle:                    caBundle,
		Labels:                      nodeClaim.Labels,
		Taints:                      nodeClaim.Spec.Taints,
		ImageGCHighThresholdPercent: kubelet.ImageGCHighThresholdPercent,
		ImageGCLowThresholdPercent:  kubelet.ImageGCLowThresholdPercent,
		ImageMinimumGCAge:           kubelet.ImageMinimumGCAge,
		KubeReserved:                kubelet.KubeReserved,
		SystemReserved:              kubelet.SystemReserved,
		FeatureGates:                kubelet.FeatureGates,
		UserData:                    nodeClass.Spec.UserData,
		ContainerRegistry:           registry.ToBootstrap(),
		CPUManagerPolicy:            kubelet.CPUManagerPolicy,
		CPUManagerPolicyOptions:     kubelet.CPUManagerPolicyOptions,
		CPUManagerReconcilePeriod:   kubelet.CPUManagerReconcilePeriod,
		MaxPods:                     kubelet.MaxPods,
	}

	userData, err := bootstrap.Generate(options)
	if err != nil && options.UserData != nil && *options.UserData != "" {
		log.FromContext(ctx).Error(err, "spec.userData contains invalid TOML, skipping merge")
		options.UserData = nil
		userData, err = bootstrap.Generate(options)
	}
	if err != nil {
		return "", fmt.Errorf("failed to generate user data: %w", err)
	}
	return userData, nil
}

func (p *Provider) getClusterCA(ctx context.Context) ([]byte, error) {
	caCert, err := p.getCACertFromClusterInfo(ctx)
	if err == nil {
		log.FromContext(ctx).V(2).Info("retrieved CA certificate from cluster-info")
		return caCert, nil
	}

	log.FromContext(ctx).V(1).Info("failed to get CA from cluster-info, trying kube-root-ca.crt", "error", err)
	return p.getCACertFromKubeRootCA(ctx)
}

func (p *Provider) getCACertFromClusterInfo(ctx context.Context) ([]byte, error) {
	cm := &v1.ConfigMap{}
	if err := p.kubeClient.Get(ctx, client.ObjectKey{
		Name:      "cluster-info",
		Namespace: "kube-public",
	}, cm); err != nil {
		return nil, fmt.Errorf("failed to get cluster-info ConfigMap: %w", err)
	}

	kubeconfig, ok := cm.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig not found in cluster-info ConfigMap")
	}

	return extractCACertFromKubeconfig(kubeconfig)
}

func (p *Provider) getCACertFromKubeRootCA(ctx context.Context) ([]byte, error) {
	cm := &v1.ConfigMap{}
	if err := p.kubeClient.Get(ctx, client.ObjectKey{
		Name:      "kube-root-ca.crt",
		Namespace: metav1.NamespaceSystem,
	}, cm); err != nil {
		return nil, fmt.Errorf("failed to get kube-root-ca.crt ConfigMap: %w", err)
	}

	caCertStr, ok := cm.Data["ca.crt"]
	if !ok {
		return nil, fmt.Errorf("ca.crt not found in kube-root-ca.crt ConfigMap")
	}

	return []byte(caCertStr), nil
}

// cluster-info (kube-public) holds a kubeadm-style kubeconfig with a single
// cluster and no contexts.
func extractCACertFromKubeconfig(kubeconfig string) ([]byte, error) {
	config, err := clientcmd.Load([]byte(kubeconfig))
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	var cluster *clientcmdapi.Cluster
	if kctx, ok := config.Contexts[config.CurrentContext]; ok {
		cluster = config.Clusters[kctx.Cluster]
	} else if len(config.Clusters) == 1 {
		for _, c := range config.Clusters {
			cluster = c
		}
	}
	if cluster == nil {
		return nil, fmt.Errorf("cannot pick a cluster from kubeconfig: %d clusters and no current-context", len(config.Clusters))
	}
	if len(cluster.CertificateAuthorityData) == 0 {
		return nil, fmt.Errorf("certificate-authority-data not found in kubeconfig")
	}
	return cluster.CertificateAuthorityData, nil
}
