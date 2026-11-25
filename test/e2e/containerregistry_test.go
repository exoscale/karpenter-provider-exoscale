package e2e_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
)

var _ = Describe("Container registry", func() {
	const name = "container-registry"

	// The registry host doesn't resolve, so the image can only be pulled
	// through the mirror the NodeClass configures in containerd.
	It("pulls images through the configured registry mirror", func(ctx SpecContext) {
		const registry = "e2e-mirror.invalid"
		spec := defaultNodeClassSpec()
		spec.ContainerRegistry = &apiv1.ContainerRegistrySpec{
			Mirrors: []apiv1.ContainerRegistryMirror{{
				Registry:  registry,
				Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{{URL: "https://mirror.gcr.io"}},
			}},
		}
		expectNodeClassReady(ctx, createNodeClass(ctx, name, spec))
		createNodePool(ctx, name)
		createDeployment(ctx, name, name, 1, func(pod *corev1.PodSpec) {
			pod.Containers[0].Image = registry + "/library/busybox:1.36"
			pod.Containers[0].Command = []string{"sleep", "infinity"}
		})

		claim := expectSingleInitializedNodeClaim(ctx, name)
		expectPodsRunningOn(ctx, name, 1, claim.Status.NodeName)
	}, SpecTimeout(20*time.Minute))
})
