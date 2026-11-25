package e2e_test

import (
	"time"

	egov3 "github.com/exoscale/egoscale/v3"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/constants"
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
)

var _ = Describe("Provisioning", Ordered, func() {
	const name = "provisioning"
	var (
		deployment *appsv1.Deployment
		instanceID egov3.UUID
	)

	BeforeAll(func(ctx SpecContext) {
		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: apiv1.ExoscaleNodeClassSpec{
				ImageTemplateSelector: &apiv1.ImageTemplateSelector{},
				DiskSize:              50,
			},
		}
		Expect(kube.Create(ctx, nodeClass)).To(Succeed())
		DeferCleanup(kube.Delete, nodeClass)
		Eventually(func(g Gomega) {
			g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass)).To(Succeed())
			g.Expect(nodeClass.StatusConditions().Root().IsTrue()).To(BeTrue(), "%+v", nodeClass.Status.Conditions)
		}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())

		nodePool := &karpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: karpv1.NodePoolSpec{
				Template: karpv1.NodeClaimTemplate{
					Spec: karpv1.NodeClaimTemplateSpec{
						NodeClassRef: &karpv1.NodeClassReference{Group: apiv1.GroupVersion.Group, Kind: "ExoscaleNodeClass", Name: name},
						Requirements: []karpv1.NodeSelectorRequirementWithMinValues{{
							Key: corev1.LabelInstanceTypeStable, Operator: corev1.NodeSelectorOpIn, Values: []string{"standard.medium"},
						}},
					},
				},
				Disruption: karpv1.Disruption{
					ConsolidationPolicy: karpv1.ConsolidationPolicyWhenEmptyOrUnderutilized,
					ConsolidateAfter:    karpv1.MustParseNillableDuration("0s"),
				},
			},
		}
		Expect(kube.Create(ctx, nodePool)).To(Succeed())
		DeferCleanup(kube.Delete, nodePool)

		deployment = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: appsv1.DeploymentSpec{
				Replicas: new(int32(1)),
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
					Spec: corev1.PodSpec{
						// Only Karpenter nodes carry this label, so the pod can't land on the system nodepool.
						NodeSelector: map[string]string{karpv1.NodePoolLabelKey: name},
						Containers: []corev1.Container{{
							Name:  "pause",
							Image: "registry.k8s.io/pause:3.10",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("500m")},
							},
						}},
					},
				},
			},
		}
		Expect(kube.Create(ctx, deployment)).To(Succeed())
		DeferCleanup(kube.Delete, deployment)
	})

	It("launches a node for a pending pod", func(ctx SpecContext) {
		var claim karpv1.NodeClaim
		Eventually(func(g Gomega) {
			var claims karpv1.NodeClaimList
			g.Expect(kube.List(ctx, &claims, client.MatchingLabels{karpv1.NodePoolLabelKey: name})).To(Succeed())
			g.Expect(claims.Items).To(HaveLen(1))
			claim = claims.Items[0]
			g.Expect(claim.StatusConditions().IsTrue(karpv1.ConditionTypeInitialized)).To(BeTrue(), "%+v", claim.Status.Conditions)
		}).WithContext(ctx).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		Eventually(func(g Gomega) {
			var pods corev1.PodList
			g.Expect(kube.List(ctx, &pods, client.InNamespace("default"), client.MatchingLabels{"app": name})).To(Succeed())
			g.Expect(pods.Items).To(HaveLen(1))
			g.Expect(pods.Items[0].Status.Phase).To(Equal(corev1.PodRunning))
			g.Expect(pods.Items[0].Spec.NodeName).To(Equal(claim.Status.NodeName))
		}).WithContext(ctx).WithTimeout(3 * time.Minute).Should(Succeed())

		id, err := utils.ParseProviderID(claim.Status.ProviderID)
		Expect(err).NotTo(HaveOccurred())
		instanceID = egov3.UUID(id)
		instance, err := exo.GetInstance(ctx, instanceID)
		Expect(err).NotTo(HaveOccurred())
		Expect(instance.Labels).To(HaveKeyWithValue(constants.InstanceLabelClusterID, cluster.ID.String()))
	}, SpecTimeout(15*time.Minute))

	It("removes the node once it is empty", func(ctx SpecContext) {
		scaleDown := client.MergeFrom(deployment.DeepCopy())
		deployment.Spec.Replicas = new(int32(0))
		Expect(kube.Patch(ctx, deployment, scaleDown)).To(Succeed())

		Eventually(func(g Gomega) {
			var claims karpv1.NodeClaimList
			g.Expect(kube.List(ctx, &claims, client.MatchingLabels{karpv1.NodePoolLabelKey: name})).To(Succeed())
			g.Expect(claims.Items).To(BeEmpty())
		}).WithContext(ctx).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())

		Eventually(func() error {
			_, err := exo.GetInstance(ctx, instanceID)
			return err
		}).WithContext(ctx).WithTimeout(5 * time.Minute).Should(MatchError(egov3.ErrNotFound))
	}, SpecTimeout(15*time.Minute))
})
