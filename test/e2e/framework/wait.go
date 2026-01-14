package framework

import (
	"context"
	"fmt"
	"time"

	"github.com/awslabs/operatorpkg/status"
	egov3 "github.com/exoscale/egoscale/v3"
	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	DefaultPollingInterval = 5 * time.Second
)

func (s *E2ESuite) GetInstance(nodeClaimName string) func() (*egov3.Instance, error) {
	return func() (*egov3.Instance, error) {
		return s.GetInstanceByNodeClaimName(context.Background(), nodeClaimName)
	}
}

func (s *E2ESuite) GetInstanceStateFunc(nodeClaimName string) func() (string, error) {
	return func() (string, error) {
		return s.GetInstanceState(context.Background(), nodeClaimName)
	}
}

func (s *E2ESuite) GetNodeClaimStatus(name string) func() (*karpenterv1.NodeClaimStatus, error) {
	return func() (*karpenterv1.NodeClaimStatus, error) {
		var nodeClaim karpenterv1.NodeClaim
		if err := s.KubeClient.Get(context.Background(), client.ObjectKey{Name: name}, &nodeClaim); err != nil {
			return nil, err
		}
		return &nodeClaim.Status, nil
	}
}

func (s *E2ESuite) GetNodeClaimProviderID(name string) func() (string, error) {
	return func() (string, error) {
		var nodeClaim karpenterv1.NodeClaim
		if err := s.KubeClient.Get(context.Background(), client.ObjectKey{Name: name}, &nodeClaim); err != nil {
			return "", err
		}
		return nodeClaim.Status.ProviderID, nil
	}
}

func (s *E2ESuite) GetNodeClassCondition(name string, conditionType string) func() (metav1.ConditionStatus, error) {
	return func() (metav1.ConditionStatus, error) {
		var nodeClass apiv1.ExoscaleNodeClass
		if err := s.KubeClient.Get(context.Background(), client.ObjectKey{Name: name}, &nodeClass); err != nil {
			return metav1.ConditionUnknown, err
		}

		cond := nodeClass.StatusConditions().Get(conditionType)
		if cond == nil {
			return metav1.ConditionUnknown, nil
		}

		return cond.Status, nil
	}
}

func (s *E2ESuite) IsNodeClassReady(name string) func() (bool, error) {
	return func() (bool, error) {
		var nodeClass apiv1.ExoscaleNodeClass
		if err := s.KubeClient.Get(context.Background(), client.ObjectKey{Name: name}, &nodeClass); err != nil {
			return false, err
		}

		cond := nodeClass.StatusConditions().Get(status.ConditionReady)
		if cond == nil {
			return false, nil
		}

		return cond.Status == metav1.ConditionTrue, nil
	}
}

func (s *E2ESuite) InstanceExistsFunc(instanceID string) func() (bool, error) {
	return func() (bool, error) {
		return s.InstanceExists(context.Background(), instanceID)
	}
}

func (s *E2ESuite) NodeExists(nodeName string) func() (bool, error) {
	return func() (bool, error) {
		var node corev1.Node
		if err := s.KubeClient.Get(context.Background(), client.ObjectKey{Name: nodeName}, &node); err != nil {
			return false, nil
		}
		return true, nil
	}
}

func (s *E2ESuite) IsNodeReady(nodeName string) func() (bool, error) {
	return func() (bool, error) {
		var node corev1.Node
		if err := s.KubeClient.Get(context.Background(), client.ObjectKey{Name: nodeName}, &node); err != nil {
			return false, err
		}

		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	}
}

func (s *E2ESuite) WaitForNodeClassReady(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for NodeClass %s to become ready", name)
		}

		ready, err := s.IsNodeClassReady(name)()
		if err != nil {
			time.Sleep(DefaultPollingInterval)
			continue
		}

		if ready {
			return nil
		}

		time.Sleep(DefaultPollingInterval)
	}
}

func (s *E2ESuite) WaitForNodeClaimDeleted(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for NodeClaim %s to be deleted", name)
		}

		var nodeClaim karpenterv1.NodeClaim
		err := s.KubeClient.Get(ctx, client.ObjectKey{Name: name}, &nodeClaim)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}
			time.Sleep(DefaultPollingInterval)
			continue
		}

		time.Sleep(DefaultPollingInterval)
	}
}

func (s *E2ESuite) WaitForInstanceDeleted(ctx context.Context, instanceID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for instance %s to be deleted", instanceID)
		}

		exists, _ := s.InstanceExists(ctx, instanceID)
		if !exists {
			return nil
		}

		time.Sleep(DefaultPollingInterval)
	}
}
