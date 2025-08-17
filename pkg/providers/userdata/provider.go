package userdata

import (
	"context"
	"encoding/base64"
	"fmt"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-exoscale/pkg/providers/userdata/bootstrap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/yaml"
)

type DefaultProvider struct {
	kubeClient      client.Client
	clusterEndpoint string
	clusterDNS      string
	clusterDomain   string
	sksBootstrap    *bootstrap.SKSBootstrap
}

func NewProvider(kubeClient client.Client, clusterEndpoint, clusterDNS, clusterDomain string) Provider {
	return &DefaultProvider{
		kubeClient:      kubeClient,
		clusterEndpoint: clusterEndpoint,
		clusterDNS:      clusterDNS,
		clusterDomain:   clusterDomain,
		sksBootstrap:    bootstrap.New(),
	}
}

func (p *DefaultProvider) Generate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodeClaim *karpenterv1.NodeClaim, options *Options) (string, error) {
	if nodeClass == nil {
		return "", fmt.Errorf("nodeClass cannot be nil")
	}
	if options == nil {
		return "", fmt.Errorf("options cannot be nil")
	}

	logger := log.FromContext(ctx).WithName("userdata").WithValues(
		"nodeClass", nodeClass.Name,
		"nodeClaim", nodeClaim.Name,
	)

	if options.ClusterEndpoint == "" {
		options.ClusterEndpoint = p.clusterEndpoint
	}
	if options.ClusterDNS == "" {
		options.ClusterDNS = p.clusterDNS
	}
	if options.ClusterDomain == "" {
		options.ClusterDomain = p.clusterDomain
	}

	if len(options.CABundle) == 0 {
		caBundle, err := p.getClusterCA(ctx)
		if err != nil {
			return "", fmt.Errorf("failed to get cluster CA: %w", err)
		}
		options.CABundle = caBundle
	}

	bootstrapOptions := &bootstrap.Options{
		ClusterName:                 options.ClusterName,
		ClusterEndpoint:             options.ClusterEndpoint,
		ClusterDNS:                  options.ClusterDNS,
		ClusterDomain:               options.ClusterDomain,
		BootstrapToken:              options.BootstrapToken,
		CABundle:                    options.CABundle,
		Labels:                      options.Labels,
		ImageGCHighThresholdPercent: options.ImageGCHighThresholdPercent,
		ImageGCLowThresholdPercent:  options.ImageGCLowThresholdPercent,
		ImageMinimumGCAge:           options.ImageMinimumGCAge,
		KubeletMaxPods:              options.KubeletMaxPods,
	}

	for _, taint := range options.Taints {
		bootstrapOptions.Taints = append(bootstrapOptions.Taints, apiv1.NodeTaint{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: string(taint.Effect),
		})
	}

	for _, taint := range nodeClaim.Spec.Taints {
		bootstrapOptions.Taints = append(bootstrapOptions.Taints, apiv1.NodeTaint{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: string(taint.Effect),
		})
	}

	if bootstrapOptions.Labels == nil {
		bootstrapOptions.Labels = make(map[string]string)
	}
	for k, v := range nodeClaim.Labels {
		bootstrapOptions.Labels[k] = v
	}

	userData, err := p.sksBootstrap.Generate(bootstrapOptions, nodeClass)
	if err != nil {
		logger.Error(err, "failed to generate user data")
		return "", fmt.Errorf("failed to generate user data: %w", err)
	}

	logger.V(2).Info("generated user data", "size", len(userData))
	return userData, nil
}

func (p *DefaultProvider) getClusterCA(ctx context.Context) ([]byte, error) {
	logger := log.FromContext(ctx).WithName("userdata").WithValues(
		"method", "getClusterCA",
	)

	caCert, err := p.getCACertFromClusterInfo(ctx)
	if err == nil {
		logger.V(2).Info("retrieved CA certificate from cluster-info")
		return caCert, nil
	}

	logger.V(1).Info("failed to get CA from cluster-info, trying kube-root-ca.crt", "error", err)
	return p.getCACertFromKubeRootCA(ctx)
}

func (p *DefaultProvider) getCACertFromClusterInfo(ctx context.Context) ([]byte, error) {
	cm := &v1.ConfigMap{}
	if err := p.kubeClient.Get(ctx, client.ObjectKey{
		Name:      "cluster-info",
		Namespace: "kube-public",
	}, cm); err != nil {
		if errors.IsNotFound(err) {
			return nil, fmt.Errorf("cluster-info ConfigMap not found")
		}
		return nil, fmt.Errorf("failed to get cluster-info ConfigMap: %w", err)
	}

	kubeconfig, ok := cm.Data["kubeconfig"]
	if !ok {
		return nil, fmt.Errorf("kubeconfig not found in cluster-info ConfigMap")
	}

	return p.extractCACertFromKubeconfig(kubeconfig)
}

func (p *DefaultProvider) getCACertFromKubeRootCA(ctx context.Context) ([]byte, error) {
	cm := &v1.ConfigMap{}
	if err := p.kubeClient.Get(ctx, client.ObjectKey{
		Name:      "kube-root-ca.crt",
		Namespace: "kube-system",
	}, cm); err != nil {
		return nil, fmt.Errorf("failed to get kube-root-ca.crt ConfigMap: %w", err)
	}

	caCertStr, ok := cm.Data["ca.crt"]
	if !ok {
		return nil, fmt.Errorf("ca.crt not found in kube-root-ca.crt ConfigMap")
	}

	return []byte(caCertStr), nil
}

func (p *DefaultProvider) extractCACertFromKubeconfig(kubeconfig string) ([]byte, error) {
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(kubeconfig), &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal kubeconfig: %w", err)
	}

	clusters, ok := config["clusters"].([]interface{})
	if !ok || len(clusters) == 0 {
		return nil, fmt.Errorf("no clusters found in kubeconfig")
	}

	cluster, ok := clusters[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid cluster format in kubeconfig")
	}

	clusterData, ok := cluster["cluster"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("cluster data not found in kubeconfig")
	}

	caCertData, ok := clusterData["certificate-authority-data"].(string)
	if !ok {
		return nil, fmt.Errorf("certificate-authority-data not found in kubeconfig")
	}

	caCert, err := base64.StdEncoding.DecodeString(caCertData)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA certificate: %w", err)
	}

	return caCert, nil
}
