package e2e_test

import (
	"context"
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
	"github.com/exoscale/karpenter-provider-exoscale/pkg/utils"
)

// Each helper registers the deletion of what it creates with DeferCleanup.

// deleteOnCleanup tolerates specs that delete obj themselves.
func deleteOnCleanup(obj client.Object) {
	DeferCleanup(func(ctx SpecContext) {
		Expect(client.IgnoreNotFound(kube.Delete(ctx, obj))).To(Succeed())
	})
}

func createNodeClass(ctx context.Context, name string, spec apiv1.ExoscaleNodeClassSpec) *apiv1.ExoscaleNodeClass {
	nodeClass := &apiv1.ExoscaleNodeClass{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: spec}
	Expect(kube.Create(ctx, nodeClass)).To(Succeed())
	deleteOnCleanup(nodeClass)
	return nodeClass
}

func expectNodeClassReady(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass) {
	Eventually(func(g Gomega) {
		g.Expect(kube.Get(ctx, client.ObjectKeyFromObject(nodeClass), nodeClass)).To(Succeed())
		g.Expect(nodeClass.StatusConditions().Root().IsTrue()).To(BeTrue(), "%+v", nodeClass.Status.Conditions)
	}).WithContext(ctx).WithTimeout(2 * time.Minute).Should(Succeed())
}

// defaultNodeClassSpec resolves the SKS template for the cluster version.
func defaultNodeClassSpec() apiv1.ExoscaleNodeClassSpec {
	return apiv1.ExoscaleNodeClassSpec{ImageTemplateSelector: &apiv1.ImageTemplateSelector{}, DiskSize: 50}
}

// createNodePool consolidates empty or underutilized nodes immediately and
// lets disruption replace every node at once; mutate can change that.
func createNodePool(ctx context.Context, name string, mutate ...func(*karpv1.NodePool)) *karpv1.NodePool {
	nodePool := &karpv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: karpv1.NodePoolSpec{
			Template: karpv1.NodeClaimTemplate{Spec: nodeClaimSpec(name)},
			Disruption: karpv1.Disruption{
				ConsolidationPolicy: karpv1.ConsolidationPolicyWhenEmptyOrUnderutilized,
				ConsolidateAfter:    karpv1.MustParseNillableDuration("0s"),
				Budgets:             []karpv1.Budget{{Nodes: "100%"}},
			},
		},
	}
	for _, m := range mutate {
		m(nodePool)
	}
	Expect(kube.Create(ctx, nodePool)).To(Succeed())
	deleteOnCleanup(nodePool)
	return nodePool
}

func nodeClaimSpec(nodeClass string) karpv1.NodeClaimTemplateSpec {
	return karpv1.NodeClaimTemplateSpec{
		NodeClassRef: &karpv1.NodeClassReference{Group: apiv1.GroupVersion.Group, Kind: "ExoscaleNodeClass", Name: nodeClass},
		Requirements: []karpv1.NodeSelectorRequirementWithMinValues{{
			Key: corev1.LabelInstanceTypeStable, Operator: corev1.NodeSelectorOpIn, Values: []string{"standard.medium"},
		}},
	}
}

// createDeployment runs pause pods that only fit on nodes of nodePool; mutate
// can change the pod spec before creation.
func createDeployment(ctx context.Context, name, nodePool string, replicas int32, mutate ...func(*corev1.PodSpec)) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(replicas),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{karpv1.NodePoolLabelKey: nodePool},
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
	for _, m := range mutate {
		m(&deployment.Spec.Template.Spec)
	}
	Expect(kube.Create(ctx, deployment)).To(Succeed())
	deleteOnCleanup(deployment)
	return deployment
}

// expectNodeReplaced patches nodeClass, then waits for the drifted node of
// nodePool to be replaced and its instance deleted. It returns the new NodeClaim.
func expectNodeReplaced(ctx context.Context, nodeClass *apiv1.ExoscaleNodeClass, nodePool string, old karpv1.NodeClaim, change func(*apiv1.ExoscaleNodeClassSpec)) karpv1.NodeClaim {
	oldInstance := instanceOf(ctx, old)
	patch := client.MergeFrom(nodeClass.DeepCopy())
	change(&nodeClass.Spec)
	Expect(kube.Patch(ctx, nodeClass, patch)).To(Succeed())

	replacement := expectSingleInitializedNodeClaim(ctx, nodePool, old.Name)
	expectInstanceGone(ctx, oldInstance.ID, 5*time.Minute)
	return replacement
}

// createExoscaleResource waits for create and registers del, which runs once
// nodePool has no NodeClaims left: attached resources can't be deleted.
func createExoscaleResource(ctx context.Context, nodePool string,
	create func() (*egov3.Operation, error),
	del func(context.Context, egov3.UUID) (*egov3.Operation, error),
) egov3.UUID {
	op, err := create()
	Expect(err).NotTo(HaveOccurred())
	op, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
	Expect(err).NotTo(HaveOccurred())
	id := op.Reference.ID
	DeferCleanup(func(ctx SpecContext) {
		expectNoNodeClaims(ctx, nodePool)
		op, err := del(ctx, id)
		Expect(err).NotTo(HaveOccurred())
		_, err = exo.Wait(ctx, op, egov3.OperationStateSuccess)
		Expect(err).NotTo(HaveOccurred())
	}, NodeTimeout(15*time.Minute))
	return id
}

func scale(ctx context.Context, deployment *appsv1.Deployment, replicas int32) {
	patch := client.MergeFrom(deployment.DeepCopy())
	deployment.Spec.Replicas = new(replicas)
	Expect(kube.Patch(ctx, deployment, patch)).To(Succeed())
}

func nodeClaims(ctx context.Context, g Gomega, nodePool string) []karpv1.NodeClaim {
	var claims karpv1.NodeClaimList
	g.Expect(kube.List(ctx, &claims, client.MatchingLabels{karpv1.NodePoolLabelKey: nodePool})).To(Succeed())
	return claims.Items
}

// expectSingleInitializedNodeClaim waits until nodePool has exactly one
// initialized NodeClaim that differs from every name in previous.
func expectSingleInitializedNodeClaim(ctx context.Context, nodePool string, previous ...string) karpv1.NodeClaim {
	var claim karpv1.NodeClaim
	Eventually(func(g Gomega) {
		claims := nodeClaims(ctx, g, nodePool)
		g.Expect(claims).To(HaveLen(1))
		claim = claims[0]
		g.Expect(previous).NotTo(ContainElement(claim.Name))
		g.Expect(claim.StatusConditions().IsTrue(karpv1.ConditionTypeInitialized)).To(BeTrue(), "%+v", claim.Status.Conditions)
	}).WithContext(ctx).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
	return claim
}

func expectNoNodeClaims(ctx context.Context, nodePool string) {
	Eventually(func(g Gomega) {
		g.Expect(nodeClaims(ctx, g, nodePool)).To(BeEmpty())
	}).WithContext(ctx).WithTimeout(10 * time.Minute).WithPolling(10 * time.Second).Should(Succeed())
}

func expectPodsRunningOn(ctx context.Context, app string, replicas int, node string) {
	Eventually(func(g Gomega) {
		var pods corev1.PodList
		g.Expect(kube.List(ctx, &pods, client.InNamespace("default"), client.MatchingLabels{"app": app})).To(Succeed())
		g.Expect(pods.Items).To(HaveLen(replicas))
		for _, pod := range pods.Items {
			g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning))
			g.Expect(pod.Spec.NodeName).To(Equal(node))
		}
	}).WithContext(ctx).WithTimeout(5 * time.Minute).Should(Succeed())
}

func instanceOf(ctx context.Context, claim karpv1.NodeClaim) *egov3.Instance {
	id, err := utils.ParseProviderID(claim.Status.ProviderID)
	Expect(err).NotTo(HaveOccurred())
	instance, err := exo.GetInstance(ctx, egov3.UUID(id))
	Expect(err).NotTo(HaveOccurred())
	return instance
}

func expectInstanceGone(ctx context.Context, id egov3.UUID, timeout time.Duration) {
	Eventually(func() error {
		_, err := exo.GetInstance(ctx, id)
		return err
	}).WithContext(ctx).WithTimeout(timeout).WithPolling(15 * time.Second).Should(MatchError(egov3.ErrNotFound))
}
