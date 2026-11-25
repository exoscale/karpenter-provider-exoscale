package e2e_test

import (
	"context"
	"encoding/json"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("Kubelet configuration", func() {
	const name = "kubelet"

	It("applies the NodeClass kubelet settings on the node", func(ctx SpecContext) {
		spec := defaultNodeClassSpec()
		spec.Kubelet = apiv1.KubeletConfiguration{
			MaxPods:          new(int32(42)),
			KubeReserved:     apiv1.KubeResourceReservation{CPU: "200m", Memory: "300Mi"},
			SystemReserved:   apiv1.SystemResourceReservation{CPU: "100m", Memory: "200Mi"},
			CPUManagerPolicy: "static",
		}
		expectNodeClassReady(ctx, createNodeClass(ctx, name, spec))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1)
		claim := expectSingleInitializedNodeClaim(ctx, name)

		config := kubeletConfig(ctx, claim.Status.NodeName)
		Expect(config.MaxPods).To(Equal(int32(42)))
		Expect(config.CPUManagerPolicy).To(Equal("static"))
		Expect(config.KubeReserved).To(HaveKeyWithValue("cpu", "200m"))
		Expect(config.KubeReserved).To(HaveKeyWithValue("memory", "300Mi"))
		Expect(config.SystemReserved).To(HaveKeyWithValue("cpu", "100m"))
		Expect(config.SystemReserved).To(HaveKeyWithValue("memory", "200Mi"))

		var node corev1.Node
		Expect(kube.Get(ctx, client.ObjectKey{Name: claim.Status.NodeName}, &node)).To(Succeed())
		Expect(node.Status.Capacity.Pods().Value()).To(Equal(int64(42)))
		// CPU has no eviction threshold, so allocatable is capacity minus both reservations.
		allocatable := node.Status.Capacity.Cpu().DeepCopy()
		allocatable.Sub(resource.MustParse("300m"))
		Expect(node.Status.Allocatable.Cpu().Cmp(allocatable)).To(Equal(0),
			"allocatable %s, capacity %s", node.Status.Allocatable.Cpu(), node.Status.Capacity.Cpu())
	}, SpecTimeout(20*time.Minute))
})

type kubeletConfiguration struct {
	MaxPods          int32             `json:"maxPods"`
	CPUManagerPolicy string            `json:"cpuManagerPolicy"`
	KubeReserved     map[string]string `json:"kubeReserved"`
	SystemReserved   map[string]string `json:"systemReserved"`
}

// kubeletConfig reads the running kubelet's configuration through the API server's node proxy.
func kubeletConfig(ctx context.Context, node string) kubeletConfiguration {
	clientset, err := kubernetes.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred())
	var raw []byte
	Eventually(func() error {
		raw, err = clientset.CoreV1().RESTClient().Get().
			Resource("nodes").Name(node).SubResource("proxy").Suffix("configz").DoRaw(ctx)
		return err
	}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())

	var configz struct {
		KubeletConfig kubeletConfiguration `json:"kubeletconfig"`
	}
	Expect(json.Unmarshal(raw, &configz)).To(Succeed())
	return configz.KubeletConfig
}
