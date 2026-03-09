package template

import (
	"context"
	"fmt"
	"regexp"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var gitVersionRegex = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)`)

// Resolver is an interface for resolving templates
type Resolver interface {
	ResolveTemplate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*Template, error)
}

type Provider struct {
	client     *egov3.Client
	zone       string
	kubeConfig *rest.Config
}

func NewResolver(client *egov3.Client, zone string, kubeConfig *rest.Config) *Provider {
	return &Provider{
		client:     client,
		zone:       zone,
		kubeConfig: kubeConfig,
	}
}

func (r *Provider) ResolveTemplate(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (*Template, error) {
	logger := log.FromContext(ctx)

	if nodeClass.Spec.TemplateID != "" {
		return &Template{
			ID: nodeClass.Spec.TemplateID,
			Labels: map[string]string{
				corev1.LabelOSStable: "linux",
			},
		}, nil
	}

	if nodeClass.Spec.ImageTemplateSelector != nil {
		selector := nodeClass.Spec.ImageTemplateSelector

		version := selector.Version
		if version == "" {
			detectedVersion, err := r.getKubernetesVersion()
			if err != nil {
				return nil, fmt.Errorf("failed to detect cluster version: %w", err)
			}
			version = detectedVersion
			logger.V(1).Info("detected cluster version", "version", version)
		}

		variant := selector.Variant
		if variant == "" {
			variant = "standard"
		}

		templateID, err := r.lookupTemplate(ctx, version, variant)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve template for version=%s, variant=%s: %w", version, variant, err)
		}

		logger.Info("resolved template from selector",
			"templateID", templateID,
			"version", version,
			"variant", variant)

		return &Template{
			ID: templateID,
			Labels: map[string]string{
				corev1.LabelOSStable: "linux",
			},
		}, nil
	}

	return nil, fmt.Errorf("neither templateID nor imageTemplateSelector is specified in NodeClass %s", nodeClass.Name)
}

func (r *Provider) getKubernetesVersion() (string, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.kubeConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create discovery client: %w", err)
	}

	serverVersion, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("failed to get server version: %w", err)
	}

	return extractSemVer(serverVersion.GitVersion)
}

func extractSemVer(gitVersion string) (string, error) {
	matches := gitVersionRegex.FindStringSubmatch(gitVersion)
	if len(matches) != 2 {
		return "", fmt.Errorf("unable to parse version from: %s", gitVersion)
	}

	return matches[1], nil
}

func (r *Provider) lookupTemplate(ctx context.Context, version, variant string) (string, error) {
	variantMap := map[string]egov3.GetActiveNodepoolTemplateVariant{
		"standard": egov3.GetActiveNodepoolTemplateVariantStandard,
		"nvidia":   egov3.GetActiveNodepoolTemplateVariantNvidia,
	}

	templateVariant, ok := variantMap[variant]
	if !ok {
		return "", fmt.Errorf("unknown template variant: %s", variant)
	}

	template, err := r.client.GetActiveNodepoolTemplate(ctx, version, templateVariant)
	if err != nil {
		return "", fmt.Errorf("failed to get active nodepool template from Exoscale API: %w", err)
	}

	return string(template.ActiveTemplate), nil
}
