package instance

import (
	"context"
	"fmt"
	"os"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	"github.com/google/uuid"
)

const (
	DefaultClusterDomain = "cluster.local"
)

type Options struct {
	Zone            string
	ClusterID       string
	InstancePrefix  string
	APIKey          string
	APISecret       string
	ClusterDomain   string
	ClusterEndpoint string
}

func NewOptionsFromEnvironment(fallbackClusterEndpoint string) (*Options, error) {
	zone, err := getRequiredEnv("EXOSCALE_ZONE")
	if err != nil {
		return nil, err
	}

	clusterID, err := getRequiredEnv("EXOSCALE_SKS_CLUSTER_ID")
	if err != nil {
		return nil, err
	}

	if _, err := uuid.Parse(clusterID); err != nil {
		return nil, fmt.Errorf("EXOSCALE_SKS_CLUSTER_ID environment variable is not a valid UUID")
	}

	instancePrefix := os.Getenv("EXOSCALE_COMPUTE_INSTANCE_PREFIX")

	APIKey, err := getRequiredEnv("EXOSCALE_API_KEY")
	if err != nil {
		return nil, err
	}

	APISecret, err := getRequiredEnv("EXOSCALE_API_SECRET")
	if err != nil {
		return nil, err
	}

	clusterDomain := os.Getenv("CLUSTER_DOMAIN")
	if clusterDomain == "" {
		clusterDomain = DefaultClusterDomain
	}

	clusterEndpoint := os.Getenv("CLUSTER_ENDPOINT")
	if clusterEndpoint == "" {
		clusterEndpoint = fallbackClusterEndpoint
	}

	return &Options{
		Zone:            zone,
		ClusterID:       clusterID,
		InstancePrefix:  instancePrefix,
		APIKey:          APIKey,
		APISecret:       APISecret,
		ClusterDomain:   clusterDomain,
		ClusterEndpoint: clusterEndpoint,
	}, nil
}

func (c *Options) BuildExoscaleClient(ctx context.Context) (*egov3.Client, error) {
	apiEndpoint, _ := os.LookupEnv("EXOSCALE_API_ENDPOINT")
	apiEnvironment, _ := os.LookupEnv("EXOSCALE_API_ENVIRONMENT")

	endpoint := getEndpoint(apiEndpoint, apiEnvironment)
	if endpoint == nil {
		exoClient, err := egov3.NewClient(credentials.NewStaticCredentials(c.APIKey, c.APISecret))
		if err != nil {
			return nil, fmt.Errorf("failed to create Exoscale client: %w", err)
		}

		autoEndpoint, err := exoClient.GetZoneAPIEndpoint(ctx, egov3.ZoneName(c.Zone))
		if err != nil {
			return nil, fmt.Errorf("failed to get zone endpoint for %s: %w", c.Zone, err)
		}

		endpoint = &autoEndpoint
	}

	exoClient, err := egov3.NewClient(
		credentials.NewStaticCredentials(c.APIKey, c.APISecret),
		egov3.ClientOptWithEndpoint(*endpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create final Exoscale client: %w", err)
	}

	if err := c.validateZone(ctx, exoClient); err != nil {
		return nil, fmt.Errorf("zone validation failed: %w", err)
	}

	return exoClient, nil
}

func getRequiredEnv(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%s environment variable is required", key)
	}
	return value, nil
}

func getEndpoint(apiEndpoint, apiEnvironment string) *egov3.Endpoint {
	if apiEndpoint != "" {
		endpoint := egov3.Endpoint(apiEndpoint)
		return &endpoint
	}

	if apiEnvironment == "ppapi" {
		endpoint := egov3.Endpoint("https://ppapi-ch-gva-2.exoscale.com/v2")
		return &endpoint
	}

	return nil
}

func (c *Options) validateZone(ctx context.Context, client *egov3.Client) error {
	zones, err := client.ListZones(ctx)
	if err != nil {
		return fmt.Errorf("failed to list zones: %w", err)
	}

	for _, z := range zones.Zones {
		if string(z.Name) == c.Zone {
			return nil
		}
	}

	var availableZones []string
	for _, z := range zones.Zones {
		availableZones = append(availableZones, string(z.Name))
	}

	return fmt.Errorf("zone %s not found. Available zones: %v", c.Zone, availableZones)
}
