package nodeclass

import (
	"context"
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func mkTLSHappySecret(t *testing.T) *corev1.Secret {
	t.Helper()
	certPEM := generateTestCertPEM(t)
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-tls", Namespace: "kube-system"},
		Data: map[string][]byte{
			"ca.crt":  certPEM,
			"tls.crt": certPEM,
			"tls.key": generateTestKeyPEM(t),
		},
	}
}

func fullNodeClass(t *testing.T, maxPods *int32) *apiv1.ExoscaleNodeClass {
	t.Helper()
	return &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{
				CPUManagerPolicy:          "static",
				CPUManagerPolicyOptions:   []string{"full-pcpus-only", "align-by-socket"},
				CPUManagerReconcilePeriod: "5s",
				MaxPods:                   maxPods,
			},
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Mirrors: []apiv1.ContainerRegistryMirror{
					{
						Registry: "docker.io",
						Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
							{
								URL:          "https://mirror.example.com",
								TLSSecretRef: &corev1.SecretReference{Name: "mirror-tls", Namespace: "kube-system"},
							},
						},
					},
				},
				Credentials: []apiv1.ContainerRegistryCredential{
					{
						Registry: "reg.example.com",
						Auth: &apiv1.ContainerRegistryAuth{
							AuthSecretRef: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "auth-secret"},
								Key:                  "auth",
							},
						},
					},
				},
			},
		},
	}
}

func TestE2E_ConfigurationHash_CombinedInputs(t *testing.T) {
	mk := func(maxPods *int32) *ExoscaleNodeClassReconciler {
		tls := mkTLSHappySecret(t)
		auth := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-secret", Namespace: "kube-system"},
			Data:       map[string][]byte{"auth": []byte("dXNlcjpwYXNz")},
		}
		nc := fullNodeClass(t, maxPods)
		return newContainerRegistryReconciler(t, nc, tls, auth)
	}

	v110 := int32(110)

	t.Run("combined contributors produce a stable hash", func(t *testing.T) {
		r1 := mk(&v110)
		r2 := mk(&v110)
		nc1 := fullNodeClass(t, &v110)
		nc2 := fullNodeClass(t, &v110)
		if err := r1.reconcileConfigurationHash(context.Background(), nc1); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := r2.reconcileConfigurationHash(context.Background(), nc2); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if nc1.Status.ConfigurationHash == "" {
			t.Fatal("hash is empty for a fully-configured NodeClass")
		}
		if nc1.Status.ConfigurationHash != nc2.Status.ConfigurationHash {
			t.Errorf("hash not deterministic: %q vs %q",
				nc1.Status.ConfigurationHash, nc2.Status.ConfigurationHash)
		}
	})

	t.Run("removing every contributor clears the hash", func(t *testing.T) {
		nc := fullNodeClass(t, &v110)
		r := newContainerRegistryReconciler(t, nc, mkTLSHappySecret(t), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-secret", Namespace: "kube-system"},
			Data:       map[string][]byte{"auth": []byte("dXNlcjpwYXNz")},
		})
		if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
			t.Fatal(err)
		}
		if nc.Status.ConfigurationHash == "" {
			t.Fatal("expected non-empty hash for fully-configured NodeClass")
		}

		nc.Spec.Kubelet = apiv1.KubeletConfiguration{}
		nc.Spec.ContainerRegistry = nil
		if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
			t.Fatal(err)
		}
		if nc.Status.ConfigurationHash != "" {
			t.Errorf("hash = %q, want empty after clearing every contributor",
				nc.Status.ConfigurationHash)
		}
	})

	t.Run("rotating a referenced Secret changes the hash", func(t *testing.T) {
		nc := fullNodeClass(t, &v110)
		tls := mkTLSHappySecret(t)
		auth := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "auth-secret", Namespace: "kube-system"},
			Data:       map[string][]byte{"auth": []byte("dXNlcjpwYXNz")},
		}
		r1 := newContainerRegistryReconciler(t, nc, tls, auth)
		nc1 := fullNodeClass(t, &v110)
		if err := r1.reconcileConfigurationHash(context.Background(), nc1); err != nil {
			t.Fatal(err)
		}

		// User rotates the auth Secret on the cluster.
		auth.Data["auth"] = []byte("cm90YXRlZDpzdHJpbmc=")
		r2 := newContainerRegistryReconciler(t, nc, tls, auth)
		nc2 := fullNodeClass(t, &v110)
		if err := r2.reconcileConfigurationHash(context.Background(), nc2); err != nil {
			t.Fatal(err)
		}

		if nc1.Status.ConfigurationHash == nc2.Status.ConfigurationHash {
			t.Errorf("hash must change when a referenced Secret rotates, both = %q",
				nc1.Status.ConfigurationHash)
		}
	})
}
