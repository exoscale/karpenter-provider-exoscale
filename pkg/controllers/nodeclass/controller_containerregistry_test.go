package nodeclass

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"sync"
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	testKeyOnce sync.Once
	testKey     *rsa.PrivateKey
	testKeyErr  error
)

func getTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	testKeyOnce.Do(func() {
		testKey, testKeyErr = rsa.GenerateKey(rand.Reader, 2048)
	})
	if testKeyErr != nil {
		t.Fatalf("generate rsa key: %v", testKeyErr)
	}
	return testKey
}

func newContainerRegistryReconciler(t *testing.T, objects ...client.Object) *ExoscaleNodeClassReconciler {
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

func TestReconcileConfigurationHash_NilSpec(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "nc"}}
	r := newContainerRegistryReconciler(t, nc)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash != "" {
		t.Errorf("expected empty hash, got %q", nc.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_EmptyRegistry(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec:       apiv1.ExoscaleNodeClassSpec{ContainerRegistry: &apiv1.ContainerRegistrySpec{}},
	}
	r := newContainerRegistryReconciler(t, nc)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// An empty registry spec contributes no entries -> empty hash, which is
	// consistent with the kubelet CPU manager and maxPods behaviour. Drift
	// still fires when the user later adds registry config (hash becomes
	// non-empty) or removes it (hash becomes empty while the NodeClaim
	// still carries a stale non-empty hash).
	if nc.Status.ConfigurationHash != "" {
		t.Errorf("expected empty hash for empty containerRegistry spec, got %q", nc.Status.ConfigurationHash)
	}
}

func TestReconcileConfigurationHash_TLSHappyPath(t *testing.T) {
	certPEM := generateTestCertPEM(t)
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Mirrors: []apiv1.ContainerRegistryMirror{
					{
						Registry: "docker.io",
						Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
							{
								URL: "https://mirror.example.com",
								TLSSecretRef: &corev1.SecretReference{
									Name:      "mirror-tls",
									Namespace: "kube-system",
								},
							},
						},
					},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-tls", Namespace: "kube-system"},
		Data: map[string][]byte{
			"ca.crt":  certPEM,
			"tls.crt": certPEM,
			"tls.key": generateTestKeyPEM(t),
		},
	}
	r := newContainerRegistryReconciler(t, nc, secret)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash == "" {
		t.Fatal("expected hash to be set")
	}
}

func TestReconcileConfigurationHash_TLSInconsistent(t *testing.T) {
	certPEM := generateTestCertPEM(t)
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Mirrors: []apiv1.ContainerRegistryMirror{
					{
						Registry: "docker.io",
						Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
							{
								URL: "https://mirror.example.com",
								TLSSecretRef: &corev1.SecretReference{
									Name:      "mirror-tls",
									Namespace: "kube-system",
								},
							},
						},
					},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-tls", Namespace: "kube-system"},
		Data:       map[string][]byte{"tls.crt": certPEM},
	}
	r := newContainerRegistryReconciler(t, nc, secret)
	err := r.reconcileConfigurationHash(context.Background(), nc)
	if err == nil || !strings.Contains(err.Error(), "TLSSecretInconsistent") {
		t.Fatalf("expected TLSSecretInconsistent, got %v", err)
	}
}

func TestReconcileConfigurationHash_TLSInvalidPEM(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Mirrors: []apiv1.ContainerRegistryMirror{
					{
						Registry: "docker.io",
						Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
							{
								URL: "https://mirror.example.com",
								TLSSecretRef: &corev1.SecretReference{
									Name: "mirror-tls",
								},
							},
						},
					},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "mirror-tls", Namespace: "kube-system"},
		Data:       map[string][]byte{"ca.crt": []byte("not-a-pem")},
	}
	r := newContainerRegistryReconciler(t, nc, secret)
	err := r.reconcileConfigurationHash(context.Background(), nc)
	if err == nil || !strings.Contains(err.Error(), "InvalidPEM") {
		t.Fatalf("expected InvalidPEM, got %v", err)
	}
}

func TestReconcileConfigurationHash_TLSMissing(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Mirrors: []apiv1.ContainerRegistryMirror{
					{
						Registry: "docker.io",
						Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
							{
								URL: "https://mirror.example.com",
								TLSSecretRef: &corev1.SecretReference{
									Name: "does-not-exist",
								},
							},
						},
					},
				},
			},
		},
	}
	r := newContainerRegistryReconciler(t, nc)
	err := r.reconcileConfigurationHash(context.Background(), nc)
	if err == nil || !strings.Contains(err.Error(), "SecretMissing") {
		t.Fatalf("expected SecretMissing, got %v", err)
	}
}

func TestReconcileConfigurationHash_TLSWrongNamespace(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Mirrors: []apiv1.ContainerRegistryMirror{
					{
						Registry: "docker.io",
						Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
							{
								URL: "https://mirror.example.com",
								TLSSecretRef: &corev1.SecretReference{
									Name:      "mirror-tls",
									Namespace: "default",
								},
							},
						},
					},
				},
			},
		},
	}
	r := newContainerRegistryReconciler(t, nc)
	err := r.reconcileConfigurationHash(context.Background(), nc)
	if err == nil || !strings.Contains(err.Error(), "SecretNamespaceInvalid") {
		t.Fatalf("expected SecretNamespaceInvalid, got %v", err)
	}
}

func TestReconcileConfigurationHash_CredentialsAllAuthMethods(t *testing.T) {
	basic := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds-basic", Namespace: "kube-system"},
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
	auth := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds-auth", Namespace: "kube-system"},
		Data:       map[string][]byte{"auth": []byte("dXNlcjpwYXNz")},
	}
	token := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds-token", Namespace: "kube-system"},
		Data:       map[string][]byte{"identitytoken": []byte("tok")},
	}
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Credentials: []apiv1.ContainerRegistryCredential{
					{
						Registry: "reg.example.com",
						Basic: &apiv1.ContainerRegistryBasicAuth{
							UsernameSecretRef: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "creds-basic"},
								Key:                  "username",
							},
							PasswordSecretRef: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "creds-basic"},
								Key:                  "password",
							},
						},
					},
					{
						Registry: "reg.example.com",
						Auth: &apiv1.ContainerRegistryAuth{
							AuthSecretRef: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "creds-auth"},
								Key:                  "auth",
							},
						},
					},
					{
						Registry: "reg.example.com",
						IdentityToken: &apiv1.ContainerRegistryIdentityToken{
							IdentityTokenSecretRef: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "creds-token"},
								Key:                  "identitytoken",
							},
						},
					},
				},
			},
		},
	}
	r := newContainerRegistryReconciler(t, nc, basic, auth, token)
	if err := r.reconcileConfigurationHash(context.Background(), nc); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nc.Status.ConfigurationHash == "" {
		t.Error("expected hash to be set")
	}
}

func TestReconcileConfigurationHash_CredentialMissing(t *testing.T) {
	nc := &apiv1.ExoscaleNodeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "nc"},
		Spec: apiv1.ExoscaleNodeClassSpec{
			ContainerRegistry: &apiv1.ContainerRegistrySpec{
				Credentials: []apiv1.ContainerRegistryCredential{
					{
						Registry: "reg.example.com",
						Auth: &apiv1.ContainerRegistryAuth{
							AuthSecretRef: corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: "missing"},
								Key:                  "auth",
							},
						},
					},
				},
			},
		},
	}
	r := newContainerRegistryReconciler(t, nc)
	err := r.reconcileConfigurationHash(context.Background(), nc)
	if err == nil || !strings.Contains(err.Error(), "SecretMissing") {
		t.Fatalf("expected SecretMissing, got %v", err)
	}
}

func TestReconcileConfigurationHash_HashIsDeterministic(t *testing.T) {
	spec := &apiv1.ContainerRegistrySpec{
		Credentials: []apiv1.ContainerRegistryCredential{
			{
				Registry: "reg.example.com",
				Auth: &apiv1.ContainerRegistryAuth{
					AuthSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "auth"},
						Key:                  "k",
					},
				},
			},
			{
				Registry: "reg2.example.com",
				Auth: &apiv1.ContainerRegistryAuth{
					AuthSecretRef: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "auth"},
						Key:                  "k",
					},
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "kube-system"},
		Data:       map[string][]byte{"k": []byte("v")},
	}
	nc1 := &apiv1.ExoscaleNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Spec: apiv1.ExoscaleNodeClassSpec{ContainerRegistry: spec}}
	nc2 := &apiv1.ExoscaleNodeClass{ObjectMeta: metav1.ObjectMeta{Name: "b"}, Spec: apiv1.ExoscaleNodeClassSpec{ContainerRegistry: spec}}
	r1 := newContainerRegistryReconciler(t, nc1, secret)
	r2 := newContainerRegistryReconciler(t, nc2, secret)
	if err := r1.reconcileConfigurationHash(context.Background(), nc1); err != nil {
		t.Fatal(err)
	}
	if err := r2.reconcileConfigurationHash(context.Background(), nc2); err != nil {
		t.Fatal(err)
	}
	if nc1.Status.ConfigurationHash != nc2.Status.ConfigurationHash {
		t.Errorf("hash not deterministic: %q vs %q", nc1.Status.ConfigurationHash, nc2.Status.ConfigurationHash)
	}
}

func TestReferencesSecret(t *testing.T) {
	spec := &apiv1.ContainerRegistrySpec{
		Mirrors: []apiv1.ContainerRegistryMirror{
			{
				Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
					{TLSSecretRef: &corev1.SecretReference{Name: "tls-a"}},
				},
			},
		},
		Credentials: []apiv1.ContainerRegistryCredential{
			{
				Basic: &apiv1.ContainerRegistryBasicAuth{
					UsernameSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "user"}},
					PasswordSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "pwd"}},
				},
			},
			{
				Auth: &apiv1.ContainerRegistryAuth{
					AuthSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}},
				},
			},
			{
				IdentityToken: &apiv1.ContainerRegistryIdentityToken{
					IdentityTokenSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tok"}},
				},
			},
		},
	}
	for _, name := range []string{"tls-a", "user", "pwd", "auth", "tok"} {
		if !referencesSecret(spec, name) {
			t.Errorf("referencesSecret(%q) = false, want true", name)
		}
	}
	if referencesSecret(spec, "missing") {
		t.Error("referencesSecret(missing) = true, want false")
	}
}

func generateTestCertPEM(t *testing.T) []byte {
	t.Helper()
	key := getTestKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func generateTestKeyPEM(t *testing.T) []byte {
	t.Helper()
	key := getTestKey(t)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}
