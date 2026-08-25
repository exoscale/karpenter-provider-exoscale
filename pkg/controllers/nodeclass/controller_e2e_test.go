package nodeclass

import (
	"context"
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// These tests exercise reconcileConfigurationHash in an end-to-end fashion:
// the NodeClass is wired with every contributor the hash can see (kubelet
// CPU manager, kubelet maxPods, container registry TLS material + creds)
// and we assert the resulting hash is deterministic, changes when any
// contributor changes, and clears when all contributors are removed.

func newE2EReconciler(t *testing.T, objects ...client.Object) *ExoscaleNodeClassReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return &ExoscaleNodeClassReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: events.NewFakeRecorder(10),
	}
}

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
		return newE2EReconciler(t, nc, tls, auth)
	}

	v110 := int32(110)
	v250 := int32(250)

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

	t.Run("changing only maxPods changes the hash", func(t *testing.T) {
		a := mk(&v110)
		b := mk(&v250)
		nca := fullNodeClass(t, &v110)
		ncb := fullNodeClass(t, &v250)
		if err := a.reconcileConfigurationHash(context.Background(), nca); err != nil {
			t.Fatal(err)
		}
		if err := b.reconcileConfigurationHash(context.Background(), ncb); err != nil {
			t.Fatal(err)
		}
		if nca.Status.ConfigurationHash == ncb.Status.ConfigurationHash {
			t.Errorf("hash must differ when maxPods changes 110 -> 250, both = %q",
				nca.Status.ConfigurationHash)
		}
	})

	t.Run("removing every contributor clears the hash", func(t *testing.T) {
		nc := fullNodeClass(t, &v110)
		r := newE2EReconciler(t, nc, mkTLSHappySecret(t), &corev1.Secret{
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
		r1 := newE2EReconciler(t, nc, tls, auth)
		nc1 := fullNodeClass(t, &v110)
		if err := r1.reconcileConfigurationHash(context.Background(), nc1); err != nil {
			t.Fatal(err)
		}

		// User rotates the auth Secret on the cluster.
		auth.Data["auth"] = []byte("cm90YXRlZDpzdHJpbmc=")
		r2 := newE2EReconciler(t, nc, tls, auth)
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

func TestE2E_ConfigurationHash_OnlyMaxPods(t *testing.T) {
	// Minimal NodeClass exercising only the maxPods path end-to-end:
	// kubelet.maxPods is the sole contributor, the hash must be non-empty,
	// change when the value changes, and clear when the override is removed.
	v110 := int32(110)
	v250 := int32(250)

	nc110 := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{MaxPods: &v110},
		},
	}
	r := newE2EReconciler(t, nc110)
	if err := r.reconcileConfigurationHash(context.Background(), nc110); err != nil {
		t.Fatal(err)
	}
	if nc110.Status.ConfigurationHash == "" {
		t.Fatal("hash empty for maxPods=110")
	}

	nc250 := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			Kubelet: apiv1.KubeletConfiguration{MaxPods: &v250},
		},
	}
	if err := r.reconcileConfigurationHash(context.Background(), nc250); err != nil {
		t.Fatal(err)
	}
	if nc250.Status.ConfigurationHash == nc110.Status.ConfigurationHash {
		t.Errorf("hash must differ between maxPods=110 and maxPods=250")
	}

	ncNone := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
	}
	if err := r.reconcileConfigurationHash(context.Background(), ncNone); err != nil {
		t.Fatal(err)
	}
	if ncNone.Status.ConfigurationHash != "" {
		t.Errorf("hash = %q, want empty when no contributor is set", ncNone.Status.ConfigurationHash)
	}
}
