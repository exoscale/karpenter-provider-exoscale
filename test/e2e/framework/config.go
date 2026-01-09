package framework

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Zone            string
	APIKey          string
	APISecret       string
	APIEndpoint     string
	TemplateID      string
	SecurityGroupID string

	TestRunID        string
	CleanupOnFailure bool

	SkipClusterCreate   bool
	SkipKarpenterDeploy bool
	ClusterName         string

	InstanceTimeout      time.Duration
	NodeJoinTimeout      time.Duration
	ClusterCreateTimeout time.Duration
}

func LoadConfigFromEnv() (*Config, error) {
	runID := generateTestRunID()

	cfg := &Config{
		Zone:                 os.Getenv("EXOSCALE_ZONE"),
		APIKey:               os.Getenv("EXOSCALE_API_KEY"),
		APISecret:            os.Getenv("EXOSCALE_API_SECRET"),
		APIEndpoint:          os.Getenv("EXOSCALE_API_ENDPOINT"),
		TemplateID:           os.Getenv("E2E_TEMPLATE_ID"),
		SecurityGroupID:      os.Getenv("E2E_SECURITY_GROUP_ID"),
		TestRunID:            runID,
		CleanupOnFailure:     os.Getenv("E2E_CLEANUP_ON_FAILURE") != "false",
		SkipClusterCreate:    os.Getenv("E2E_SKIP_CLUSTER_CREATE") == "true",
		SkipKarpenterDeploy:  os.Getenv("E2E_SKIP_KARPENTER_DEPLOY") == "true",
		ClusterName:          os.Getenv("E2E_CLUSTER_NAME"),
		InstanceTimeout:      5 * time.Minute,
		NodeJoinTimeout:      10 * time.Minute,
		ClusterCreateTimeout: 15 * time.Minute,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Validate() error {
	if c.Zone == "" {
		return fmt.Errorf("EXOSCALE_ZONE is required")
	}
	if c.APIKey == "" {
		return fmt.Errorf("EXOSCALE_API_KEY is required")
	}
	if c.APISecret == "" {
		return fmt.Errorf("EXOSCALE_API_SECRET is required")
	}
	if c.TemplateID == "" {
		return fmt.Errorf("E2E_TEMPLATE_ID is required")
	}
	if c.SecurityGroupID == "" {
		return fmt.Errorf("E2E_SECURITY_GROUP_ID is required")
	}
	if c.SkipClusterCreate && c.ClusterName == "" {
		return fmt.Errorf("E2E_CLUSTER_NAME is required when E2E_SKIP_CLUSTER_CREATE=true")
	}
	return nil
}

func generateTestRunID() string {
	timestamp := time.Now().Format("20060102-150405")
	randomBytes := make([]byte, 3)
	rand.Read(randomBytes)
	randomSuffix := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("e2e-%s-%s", timestamp, randomSuffix)
}
