package framework

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	"github.com/onsi/ginkgo/v2"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type SKSCluster struct {
	ID       egov3.UUID
	Name     string
	Endpoint string
}

func (s *E2ESuite) CreateSKSCluster(ctx context.Context) (*SKSCluster, error) {
	clusterName := s.Config.TestRunID

	versions, err := s.ExoClient.ListSKSClusterVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list SKS versions: %w", err)
	}

	if len(versions.SKSClusterVersions) == 0 {
		return nil, fmt.Errorf("no SKS versions available")
	}
	latestVersion := versions.SKSClusterVersions[0]

	ginkgo.GinkgoWriter.Printf("Creating SKS cluster %s with version %s...\n", clusterName, latestVersion)

	autoUpgrade := false
	req := egov3.CreateSKSClusterRequest{
		Name:        clusterName,
		Level:       egov3.CreateSKSClusterRequestLevelStarter,
		Cni:         egov3.CreateSKSClusterRequestCniCilium,
		Version:     latestVersion,
		AutoUpgrade: &autoUpgrade,
	}

	op, err := s.ExoClient.CreateSKSCluster(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to create SKS cluster: %w", err)
	}

	op, err = s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess)
	if err != nil {
		return nil, fmt.Errorf("failed to wait for SKS cluster creation: %w", err)
	}

	if op.Reference == nil {
		return nil, fmt.Errorf("operation reference is nil")
	}

	clusterID := op.Reference.ID
	s.TrackSKSCluster(clusterID.String())

	cluster, err := s.ExoClient.GetSKSCluster(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get SKS cluster: %w", err)
	}

	ginkgo.GinkgoWriter.Printf("Created SKS cluster %s (%s) at %s\n", cluster.Name, cluster.ID, cluster.Endpoint)

	return &SKSCluster{
		ID:       cluster.ID,
		Name:     cluster.Name,
		Endpoint: cluster.Endpoint,
	}, nil
}

func (s *E2ESuite) WaitForSKSClusterReady(ctx context.Context, clusterID egov3.UUID, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for SKS cluster to be ready")
		}

		cluster, err := s.ExoClient.GetSKSCluster(ctx, clusterID)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		if cluster.State == egov3.SKSClusterStateRunning {
			ginkgo.GinkgoWriter.Printf("SKS cluster %s is ready\n", cluster.Name)
			return nil
		}

		ginkgo.GinkgoWriter.Printf("Waiting for SKS cluster to be ready (current state: %s)...\n", cluster.State)
		time.Sleep(10 * time.Second)
	}
}

func (s *E2ESuite) GetSKSClusterKubeconfig(ctx context.Context, clusterID egov3.UUID) (*rest.Config, error) {
	ttl := int64(86400)
	req := egov3.SKSKubeconfigRequest{
		User:   "e2e-admin",
		Groups: []string{"system:masters"},
		Ttl:    ttl,
	}

	resp, err := s.ExoClient.GenerateSKSClusterKubeconfig(ctx, clusterID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to generate kubeconfig: %w", err)
	}

	kubeconfigData, err := base64.StdEncoding.DecodeString(resp.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to decode kubeconfig: %w", err)
	}

	clientConfig, err := clientcmd.NewClientConfigFromBytes(kubeconfigData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get rest config: %w", err)
	}

	return restConfig, nil
}

func (s *E2ESuite) DeleteSKSCluster(ctx context.Context, clusterID egov3.UUID) error {
	cluster, err := s.ExoClient.GetSKSCluster(ctx, clusterID)
	if err != nil {
		return nil
	}

	for _, nodepool := range cluster.Nodepools {
		ginkgo.GinkgoWriter.Printf("Deleting SKS nodepool %s...\n", nodepool.Name)
		op, err := s.ExoClient.DeleteSKSNodepool(ctx, clusterID, nodepool.ID)
		if err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to delete nodepool %s: %v\n", nodepool.Name, err)
			continue
		}
		if _, err := s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to wait for nodepool deletion: %v\n", err)
		}
	}

	ginkgo.GinkgoWriter.Printf("Deleting SKS cluster %s...\n", cluster.Name)
	op, err := s.ExoClient.DeleteSKSCluster(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("failed to delete SKS cluster: %w", err)
	}

	if _, err := s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
		return fmt.Errorf("failed to wait for SKS cluster deletion: %w", err)
	}

	ginkgo.GinkgoWriter.Printf("Deleted SKS cluster %s\n", cluster.Name)
	return nil
}
