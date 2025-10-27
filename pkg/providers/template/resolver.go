package template

import (
	"context"
	"fmt"
	"regexp"
	"sync"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var gitVersionRegex = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)`)

type Resolver interface {
	ResolveTemplateID(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) (string, error)
}

type exoscaleClient interface {
	GetActiveNodepoolTemplate(ctx context.Context, version string, variant egov3.GetActiveNodepoolTemplateVariant) (*egov3.GetActiveNodepoolTemplateResponse, error)
}

type templateCacheEntry struct {
	templateID string
	cachedAt   time.Time
}

type DefaultResolver struct {
	client     exoscaleClient
	zone       string
	kubeConfig *rest.Config

	// Cache for template lookups to avoid excessive API calls
	cacheMu    sync.RWMutex
	cache      map[string]templateCacheEntry // key: "version:variant"
	cacheTTL   time.Duration
}

func NewResolver(client *egov3.Client, zone string, kubeConfig *rest.Config) Resolver {
	return &DefaultResolver{
		client:     client,
		zone:       zone,
		kubeConfig: kubeConfig,
		cache:      make(map[string]templateCacheEntry),
		cacheTTL:   5 * time.Minute,
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

	return extractSemVer(serverVersion.GitVersion)
}

func extractSemVer(gitVersion string) (string, error) {
	matches := gitVersionRegex.FindStringSubmatch(gitVersion)
	if len(matches) != 2 {
		return "", fmt.Errorf("unable to parse version from: %s", gitVersion)
	}

	return matches[1], nil
}

func (r *DefaultResolver) lookupTemplate(ctx context.Context, version, variant string) (string, error) {
	logger := log.FromContext(ctx)
	cacheKey := fmt.Sprintf("%s:%s", version, variant)

	r.cacheMu.RLock()
	if entry, found := r.cache[cacheKey]; found {
		if time.Since(entry.cachedAt) < r.cacheTTL {
			r.cacheMu.RUnlock()
			logger.V(2).Info("using cached template ID", "cacheKey", cacheKey, "templateID", entry.templateID)
			return entry.templateID, nil
		}
	}
	r.cacheMu.RUnlock()

	logger.V(1).Info("cache miss, fetching template from API", "cacheKey", cacheKey)

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

	templateID := string(template.ActiveTemplate)

	r.cacheMu.Lock()
	r.cache[cacheKey] = templateCacheEntry{
		templateID: templateID,
		cachedAt:   time.Now(),
	}
	r.cacheMu.Unlock()

	return templateID, nil
}
