package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	KarpenterNamespace   = "kube-system"
	KarpenterImage       = "docker.io/exoscale/karpenter-exoscale:latest"
	KarpenterSecretName  = "karpenter-exoscale-credentials"
	KarpenterReadyTimout = 5 * time.Minute
)

func (s *E2ESuite) DeployKarpenter(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Deploying Karpenter to cluster...")

	if err := s.deployCRDs(ctx); err != nil {
		return fmt.Errorf("failed to deploy CRDs: %w", err)
	}

	if err := s.deployRBAC(ctx); err != nil {
		return fmt.Errorf("failed to deploy RBAC: %w", err)
	}

	if err := s.createCredentialsSecret(ctx); err != nil {
		return fmt.Errorf("failed to create credentials secret: %w", err)
	}

	if err := s.deployKarpenterDeployment(ctx); err != nil {
		return fmt.Errorf("failed to deploy Karpenter: %w", err)
	}

	if err := s.waitForKarpenterReady(ctx, KarpenterReadyTimout); err != nil {
		return fmt.Errorf("karpenter not ready: %w", err)
	}

	ginkgo.GinkgoWriter.Println("Karpenter deployed successfully")
	return nil
}

func (s *E2ESuite) deployCRDs(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Deploying CRDs...")

	crdFiles := []string{
		"test/e2e/crds/karpenter.sh_nodeclaims.yaml",
		"test/e2e/crds/karpenter.sh_nodepools.yaml",
		"test/e2e/crds/karpenter.exoscale.com_exoscalenodeclasses.yaml",
	}

	for _, crdFile := range crdFiles {
		if _, err := os.Stat(crdFile); err != nil {
			return fmt.Errorf("CRD file not found: %s (run from repository root)", crdFile)
		}
		if err := s.applyCRDFromFile(ctx, crdFile); err != nil {
			return fmt.Errorf("failed to apply CRD %s: %w", crdFile, err)
		}
	}

	return nil
}

func (s *E2ESuite) applyCRDFromFile(ctx context.Context, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		absPath, _ := filepath.Abs(filePath)
		return fmt.Errorf("failed to read CRD file %s (abs: %s): %w", filePath, absPath, err)
	}

	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(data, &crd); err != nil {
		return fmt.Errorf("failed to unmarshal CRD: %w", err)
	}

	existing := &apiextensionsv1.CustomResourceDefinition{}
	err = s.KubeClient.Get(ctx, client.ObjectKey{Name: crd.Name}, existing)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			if err := s.KubeClient.Create(ctx, &crd); err != nil {
				return fmt.Errorf("failed to create CRD %s: %w", crd.Name, err)
			}
			ginkgo.GinkgoWriter.Printf("Created CRD: %s\n", crd.Name)
		} else {
			return fmt.Errorf("failed to get CRD %s: %w", crd.Name, err)
		}
	} else {
		crd.ResourceVersion = existing.ResourceVersion
		if err := s.KubeClient.Update(ctx, &crd); err != nil {
			return fmt.Errorf("failed to update CRD %s: %w", crd.Name, err)
		}
		ginkgo.GinkgoWriter.Printf("Updated CRD: %s\n", crd.Name)
	}

	return nil
}

func (s *E2ESuite) deployRBAC(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Deploying RBAC...")

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "karpenter",
			Namespace: KarpenterNamespace,
		},
	}
	if err := s.createOrUpdate(ctx, sa); err != nil {
		return fmt.Errorf("failed to create ServiceAccount: %w", err)
	}
	ginkgo.GinkgoWriter.Println("Created ServiceAccount: karpenter")

	clusterRole := s.buildClusterRole()
	if err := s.createOrUpdate(ctx, clusterRole); err != nil {
		return fmt.Errorf("failed to create ClusterRole: %w", err)
	}
	ginkgo.GinkgoWriter.Println("Created ClusterRole: karpenter")

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "karpenter",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "karpenter",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "karpenter",
				Namespace: KarpenterNamespace,
			},
		},
	}
	if err := s.createOrUpdate(ctx, clusterRoleBinding); err != nil {
		return fmt.Errorf("failed to create ClusterRoleBinding: %w", err)
	}
	ginkgo.GinkgoWriter.Println("Created ClusterRoleBinding: karpenter")

	return nil
}

func (s *E2ESuite) buildClusterRole() *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "karpenter",
		},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"nodes"}, Verbs: []string{"delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"configmaps"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"get", "list", "watch", "create", "update"}},
			{APIGroups: []string{"apps"}, Resources: []string{"daemonsets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"policy"}, Resources: []string{"poddisruptionbudgets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"karpenter.exoscale.com"}, Resources: []string{"exoscalenodeclasses"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{"karpenter.exoscale.com"}, Resources: []string{"exoscalenodeclasses/finalizers"}, Verbs: []string{"update"}},
			{APIGroups: []string{"karpenter.exoscale.com"}, Resources: []string{"exoscalenodeclasses/status"}, Verbs: []string{"get", "patch", "update"}},
			{APIGroups: []string{"karpenter.sh"}, Resources: []string{"nodeclaims"}, Verbs: []string{"create", "delete", "get", "list", "patch", "update", "watch"}},
			{APIGroups: []string{"karpenter.sh"}, Resources: []string{"nodeclaims/status"}, Verbs: []string{"get", "patch", "update"}},
			{APIGroups: []string{"karpenter.sh"}, Resources: []string{"nodepools"}, Verbs: []string{"get", "list", "watch", "create", "update", "delete", "patch"}},
			{APIGroups: []string{"karpenter.sh"}, Resources: []string{"nodepools/status"}, Verbs: []string{"get", "patch", "update", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods", "persistentvolumes", "persistentvolumeclaims"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"pods/eviction"}, Verbs: []string{"create"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"volumeattachments"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"csinodes"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
}

func (s *E2ESuite) createCredentialsSecret(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Creating credentials secret...")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KarpenterSecretName,
			Namespace: KarpenterNamespace,
		},
		StringData: map[string]string{
			"api-key":    s.Config.APIKey,
			"api-secret": s.Config.APISecret,
		},
	}

	if err := s.createOrUpdate(ctx, secret); err != nil {
		return err
	}

	ginkgo.GinkgoWriter.Println("Created credentials secret")
	return nil
}

func (s *E2ESuite) deployKarpenterDeployment(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Deploying Karpenter deployment...")

	replicas := int32(1)
	runAsUser := int64(1997)
	fsGroup := int64(2000)
	runAsNonRoot := true
	allowPrivilegeEscalation := false

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "karpenter-exoscale",
			Namespace: KarpenterNamespace,
			Labels: map[string]string{
				"app": "karpenter-exoscale",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "karpenter-exoscale",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "karpenter-exoscale",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "karpenter",
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &runAsNonRoot,
						RunAsUser:    &runAsUser,
						FSGroup:      &fsGroup,
					},
					Containers: []corev1.Container{
						{
							Name:            "karpenter",
							Image:           KarpenterImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: &allowPrivilegeEscalation,
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Ports: []corev1.ContainerPort{
								{Name: "metrics", ContainerPort: 8080, Protocol: corev1.ProtocolTCP},
								{Name: "http", ContainerPort: 8081, Protocol: corev1.ProtocolTCP},
							},
							Env: []corev1.EnvVar{
								{Name: "EXOSCALE_ZONE", Value: s.Config.Zone},
								{Name: "EXOSCALE_SKS_CLUSTER_ID", Value: s.SKSClusterID.String()},
								{
									Name: "EXOSCALE_API_KEY",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: KarpenterSecretName},
											Key:                  "api-key",
										},
									},
								},
								{
									Name: "EXOSCALE_API_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: KarpenterSecretName},
											Key:                  "api-secret",
										},
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromString("http"),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       15,
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "node-role.kubernetes.io/control-plane",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
						{
							Key:      "node.kubernetes.io/not-ready",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}

	if s.Config.APIEndpoint != "" {
		deployment.Spec.Template.Spec.Containers[0].Env = append(
			deployment.Spec.Template.Spec.Containers[0].Env,
			corev1.EnvVar{Name: "EXOSCALE_API_ENDPOINT", Value: s.Config.APIEndpoint},
		)
	}

	if err := s.createOrUpdate(ctx, deployment); err != nil {
		return err
	}

	ginkgo.GinkgoWriter.Println("Created Karpenter deployment")
	return nil
}

func (s *E2ESuite) waitForKarpenterReady(ctx context.Context, timeout time.Duration) error {
	ginkgo.GinkgoWriter.Println("Waiting for Karpenter to be ready...")

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for Karpenter to be ready")
		}

		var deployment appsv1.Deployment
		if err := s.KubeClient.Get(ctx, client.ObjectKey{
			Namespace: KarpenterNamespace,
			Name:      "karpenter-exoscale",
		}, &deployment); err != nil {
			time.Sleep(5 * time.Second)
			continue
		}

		if deployment.Status.ReadyReplicas > 0 && deployment.Status.ReadyReplicas == deployment.Status.Replicas {
			ginkgo.GinkgoWriter.Printf("Karpenter is ready (%d/%d replicas)\n",
				deployment.Status.ReadyReplicas, deployment.Status.Replicas)
			return nil
		}

		ginkgo.GinkgoWriter.Printf("Waiting for Karpenter (ready: %d/%d)...\n",
			deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
		time.Sleep(5 * time.Second)
	}
}

func (s *E2ESuite) createOrUpdate(ctx context.Context, obj client.Object) error {
	existing := obj.DeepCopyObject().(client.Object)
	err := s.KubeClient.Get(ctx, client.ObjectKeyFromObject(obj), existing)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return s.KubeClient.Create(ctx, obj)
		}
		return err
	}

	obj.SetResourceVersion(existing.GetResourceVersion())
	return s.KubeClient.Update(ctx, obj)
}

func (s *E2ESuite) DeleteKarpenter(ctx context.Context) error {
	ginkgo.GinkgoWriter.Println("Deleting Karpenter resources...")

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "karpenter-exoscale",
			Namespace: KarpenterNamespace,
		},
	}
	if err := s.KubeClient.Delete(ctx, deployment); err != nil && !k8serrors.IsNotFound(err) {
		ginkgo.GinkgoWriter.Printf("Warning: failed to delete Karpenter deployment: %v\n", err)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KarpenterSecretName,
			Namespace: KarpenterNamespace,
		},
	}
	if err := s.KubeClient.Delete(ctx, secret); err != nil && !k8serrors.IsNotFound(err) {
		ginkgo.GinkgoWriter.Printf("Warning: failed to delete credentials secret: %v\n", err)
	}

	ginkgo.GinkgoWriter.Println("Karpenter resources deleted")
	return nil
}
