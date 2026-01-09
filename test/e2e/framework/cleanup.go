package framework

import (
	"context"
	"slices"
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/onsi/ginkgo/v2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

func (s *E2ESuite) CleanupAllResources(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	ginkgo.GinkgoWriter.Println("Starting cleanup of test resources...")

	s.cleanupNodeClaims(ctx)
	s.cleanupNodePools(ctx)
	s.cleanupNodeClasses(ctx)

	s.waitForInstancesDeletion(ctx, 5*time.Minute)

	s.forceCleanupTrackedInstances(ctx)
	s.cleanupPrivateNetworks(ctx)

	ginkgo.GinkgoWriter.Println("Cleanup completed")
}

func (s *E2ESuite) cleanupNodeClaims(ctx context.Context) {
	s.mu.Lock()
	uids := make([]types.UID, len(s.createdNodeClaims))
	copy(uids, s.createdNodeClaims)
	s.mu.Unlock()

	if len(uids) == 0 {
		return
	}

	var list karpenterv1.NodeClaimList
	if err := s.KubeClient.List(ctx, &list); err != nil {
		ginkgo.GinkgoWriter.Printf("Warning: failed to list NodeClaims: %v\n", err)
		return
	}

	for _, item := range list.Items {
		if !slices.Contains(uids, item.UID) {
			continue
		}

		if err := s.KubeClient.Delete(ctx, &item); err != nil {
			if !errors.IsNotFound(err) {
				ginkgo.GinkgoWriter.Printf("Warning: failed to delete NodeClaim %s: %v\n", item.Name, err)
			}
		} else {
			ginkgo.GinkgoWriter.Printf("Deleted NodeClaim: %s\n", item.Name)
		}
	}
}

func (s *E2ESuite) cleanupNodePools(ctx context.Context) {
	s.mu.Lock()
	uids := make([]types.UID, len(s.createdNodePools))
	copy(uids, s.createdNodePools)
	s.mu.Unlock()

	if len(uids) == 0 {
		return
	}

	var list karpenterv1.NodePoolList
	if err := s.KubeClient.List(ctx, &list); err != nil {
		ginkgo.GinkgoWriter.Printf("Warning: failed to list NodePools: %v\n", err)
		return
	}

	for _, item := range list.Items {
		if !slices.Contains(uids, item.UID) {
			continue
		}

		if err := s.KubeClient.Delete(ctx, &item); err != nil {
			if !errors.IsNotFound(err) {
				ginkgo.GinkgoWriter.Printf("Warning: failed to delete NodePool %s: %v\n", item.Name, err)
			}
		} else {
			ginkgo.GinkgoWriter.Printf("Deleted NodePool: %s\n", item.Name)
		}
	}
}

func (s *E2ESuite) cleanupNodeClasses(ctx context.Context) {
	s.mu.Lock()
	uids := make([]types.UID, len(s.createdNodeClasses))
	copy(uids, s.createdNodeClasses)
	s.mu.Unlock()

	if len(uids) == 0 {
		return
	}

	var list apiv1.ExoscaleNodeClassList
	if err := s.KubeClient.List(ctx, &list); err != nil {
		ginkgo.GinkgoWriter.Printf("Warning: failed to list NodeClasses: %v\n", err)
		return
	}

	for _, item := range list.Items {
		if !slices.Contains(uids, item.UID) {
			continue
		}

		if err := s.KubeClient.Delete(ctx, &item); err != nil {
			if !errors.IsNotFound(err) {
				ginkgo.GinkgoWriter.Printf("Warning: failed to delete NodeClass %s: %v\n", item.Name, err)
			}
		} else {
			ginkgo.GinkgoWriter.Printf("Deleted NodeClass: %s\n", item.Name)
		}
	}
}

func (s *E2ESuite) waitForInstancesDeletion(ctx context.Context, timeout time.Duration) {
	s.mu.Lock()
	instances := make([]string, len(s.createdInstances))
	copy(instances, s.createdInstances)
	s.mu.Unlock()

	if len(instances) == 0 {
		return
	}

	ginkgo.GinkgoWriter.Printf("Waiting up to %v for %d instance(s) to be deleted by Karpenter...\n", timeout, len(instances))

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			ginkgo.GinkgoWriter.Println("Timeout waiting for instance deletion, will force cleanup")
			return
		}

		allDeleted := true
		for _, instanceID := range instances {
			exists, _ := s.InstanceExists(ctx, instanceID)
			if exists {
				allDeleted = false
				break
			}
		}

		if allDeleted {
			ginkgo.GinkgoWriter.Println("All instances deleted by Karpenter")
			return
		}

		time.Sleep(10 * time.Second)
	}
}

func (s *E2ESuite) forceCleanupTrackedInstances(ctx context.Context) {
	s.mu.Lock()
	instances := make([]string, len(s.createdInstances))
	copy(instances, s.createdInstances)
	s.mu.Unlock()

	for _, instanceID := range instances {
		exists, _ := s.InstanceExists(ctx, instanceID)
		if !exists {
			continue
		}

		ginkgo.GinkgoWriter.Printf("Force-deleting orphaned instance: %s\n", instanceID)
		op, err := s.ExoClient.DeleteInstance(ctx, egov3.UUID(instanceID))
		if err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to delete instance %s: %v\n", instanceID, err)
			continue
		}
		if _, err := s.ExoClient.Wait(ctx, op, egov3.OperationStateSuccess); err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to wait for instance deletion %s: %v\n", instanceID, err)
		}
	}
}

func (s *E2ESuite) cleanupPrivateNetworks(ctx context.Context) {
	s.mu.Lock()
	networks := make([]string, len(s.createdPrivateNetworks))
	copy(networks, s.createdPrivateNetworks)
	s.mu.Unlock()

	for _, networkID := range networks {
		if err := s.DeletePrivateNetwork(ctx, networkID); err != nil {
			ginkgo.GinkgoWriter.Printf("Warning: failed to delete private network %s: %v\n", networkID, err)
		}
	}
}

func (s *E2ESuite) DeleteNodeClass(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) error {
	return s.KubeClient.Delete(ctx, nodeClass)
}

func (s *E2ESuite) DeleteNodeClaim(ctx context.Context, nodeClaim *karpenterv1.NodeClaim) error {
	return s.KubeClient.Delete(ctx, nodeClaim)
}

func (s *E2ESuite) DeleteNodePool(ctx context.Context, nodePool *karpenterv1.NodePool) error {
	return s.KubeClient.Delete(ctx, nodePool)
}
