package template

import (
	"context"
	"fmt"
	"regexp"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Resolver interface {
	ResolveTemplateID(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (string, error)
}

type DefaultResolver struct {
	client     *egov3.Client
	zone       string
	kubeConfig *rest.Config
}

func NewResolver(client *egov3.Client, zone string, kubeConfig *rest.Config) Resolver {
	return &DefaultResolver{
		client:     client,
		zone:       zone,
		kubeConfig: kubeConfig,
	}
}

func (r *DefaultResolver) ResolveTemplateID(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (string, error) {
	logger := log.FromContext(ctx)

	if nodeClass.Spec.TemplateID != "" {
		return nodeClass.Spec.TemplateID, nil
	}

	if nodeClass.Spec.ImageTemplateSelector != nil {
		selector := nodeClass.Spec.ImageTemplateSelector

		version := selector.Version
		if version == "" {
			detectedVersion, err := r.getKubernetesVersion()
			if err != nil {
				return "", fmt.Errorf("failed to detect cluster version: %w", err)
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
			return "", fmt.Errorf("failed to resolve template for version=%s, variant=%s: %w", version, variant, err)
		}

		logger.Info("resolved template from selector",
			"templateID", templateID,
			"version", version,
			"variant", variant)

		return templateID, nil
	}

	return "", fmt.Errorf("neither templateID nor imageTemplateSelector is specified in NodeClass %s", nodeClass.Name)
}

func (r *DefaultResolver) getKubernetesVersion() (string, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(r.kubeConfig)
	if err != nil {
		return "", fmt.Errorf("failed to create discovery client: %w", err)
	}

	serverVersion, err := discoveryClient.ServerVersion()
	if err != nil {
		return "", fmt.Errorf("failed to get server version: %w", err)
	}

	// Extract semver from GitVersion (e.g., "v1.33.2-eks.1" -> "1.33.2")
	re := regexp.MustCompile(`^v?(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(serverVersion.GitVersion)
	if len(matches) != 2 {
		return "", fmt.Errorf("unable to parse version from: %s", serverVersion.GitVersion)
	}

	return matches[1], nil
}

func (r *DefaultResolver) lookupTemplate(ctx context.Context, version, variant string) (string, error) {
	templateVariant := egov3.GetActiveNodepoolTemplateVariantStandard

	if variant != "nvidia" {
		templateVariant = egov3.GetActiveNodepoolTemplateVariantNvidia
	} else if variant != "standard" {
		return "", fmt.Errorf("unknown template variant: %s", variant)
	}

	template, err := r.client.GetActiveNodepoolTemplate(ctx, version, templateVariant)
	if err != nil {
		return "", fmt.Errorf("failed to get active nodepool template from Exoscale API: %w", err)
	}

	return string(template.ActiveTemplate), nil
}
