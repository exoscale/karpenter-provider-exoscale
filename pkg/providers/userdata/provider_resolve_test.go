package userdata

import (
	"context"
	"encoding/pem"
	"strings"
	"testing"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func pemBlock(typ string, payload []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: payload})
}

func newResolveTestProvider(t *testing.T, objs ...client.Object) *Provider {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &Provider{kubeClient: c}
}

func TestResolveContainerRegistry_NilSpec(t *testing.T) {
	p := newResolveTestProvider(t)
	got, err := p.RegistryResolver().Resolve(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for nil spec, got %+v", got)
	}
}

func TestResolveContainerRegistry_TLSHappyPath(t *testing.T) {
	certPEM := pemBlock("CERTIFICATE", []byte("cert-bytes"))
	keyPEM := pemBlock("PRIVATE KEY", []byte("key-bytes"))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tls", Namespace: "kube-system"},
		Data: map[string][]byte{
			"ca.crt":  certPEM,
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
	spec := &apiv1.ContainerRegistrySpec{
		Mirrors: []apiv1.ContainerRegistryMirror{
			{
				Registry: "docker.io",
				Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
					{
						URL: "https://mirror.example.com",
						TLSSecretRef: &corev1.SecretReference{
							Name: "tls",
						},
					},
				},
			},
		},
	}
	p := newResolveTestProvider(t, secret)
	got, err := p.RegistryResolver().Resolve(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Mirrors) != 1 || got.Mirrors[0].Registry != "docker.io" {
		t.Errorf("unexpected mirrors: %+v", got.Mirrors)
	}
	tls := got.Mirrors[0].Endpoints[0].TLS
	if tls == nil {
		t.Fatalf("expected TLS to be set")
	}
	if len(tls.CA) == 0 || len(tls.Cert) == 0 || len(tls.Key) == 0 {
		t.Errorf("expected non-empty ca/cert/key, got %+v", tls)
	}
}

func TestResolveContainerRegistry_TLSInconsistent(t *testing.T) {
	certPEM := pemBlock("CERTIFICATE", []byte("cert-bytes"))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tls", Namespace: "kube-system"},
		Data:       map[string][]byte{"tls.crt": certPEM},
	}
	spec := &apiv1.ContainerRegistrySpec{
		Mirrors: []apiv1.ContainerRegistryMirror{
			{
				Registry: "docker.io",
				Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
					{
						URL:          "https://mirror.example.com",
						TLSSecretRef: &corev1.SecretReference{Name: "tls"},
					},
				},
			},
		},
	}
	p := newResolveTestProvider(t, secret)
	_, err := p.RegistryResolver().Resolve(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "tls.crt") {
		t.Fatalf("expected tls.crt/tls.key error, got %v", err)
	}
}

func TestResolveContainerRegistry_TLSOnlyCA(t *testing.T) {
	certPEM := pemBlock("CERTIFICATE", []byte("cert-bytes"))
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tls", Namespace: "kube-system"},
		Data:       map[string][]byte{"ca.crt": certPEM},
	}
	spec := &apiv1.ContainerRegistrySpec{
		Mirrors: []apiv1.ContainerRegistryMirror{
			{
				Registry: "docker.io",
				Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
					{
						URL:          "https://mirror.example.com",
						TLSSecretRef: &corev1.SecretReference{Name: "tls"},
					},
				},
			},
		},
	}
	p := newResolveTestProvider(t, secret)
	got, err := p.RegistryResolver().Resolve(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	tls := got.Mirrors[0].Endpoints[0].TLS
	if len(tls.CA) == 0 {
		t.Errorf("expected ca to be set, got %+v", tls)
	}
	if len(tls.Cert) != 0 || len(tls.Key) != 0 {
		t.Errorf("expected cert/key empty for ca-only secret, got %+v", tls)
	}
}

func TestResolveContainerRegistry_MissingSecret(t *testing.T) {
	spec := &apiv1.ContainerRegistrySpec{
		Mirrors: []apiv1.ContainerRegistryMirror{
			{
				Registry: "docker.io",
				Endpoints: []apiv1.ContainerRegistryMirrorEndpoint{
					{
						URL:          "https://mirror.example.com",
						TLSSecretRef: &corev1.SecretReference{Name: "missing"},
					},
				},
			},
		},
	}
	p := newResolveTestProvider(t)
	_, err := p.RegistryResolver().Resolve(context.Background(), spec)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func TestResolveContainerRegistry_CredentialsAllStyles(t *testing.T) {
	basic := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "basic", Namespace: "kube-system"},
		Data:       map[string][]byte{"username": []byte("u"), "password": []byte("p")},
	}
	auth := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "kube-system"},
		Data:       map[string][]byte{"auth": []byte("dXNlcjpwYXNz")},
	}
	token := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tok", Namespace: "kube-system"},
		Data:       map[string][]byte{"identitytoken": []byte("oa-token")},
	}
	spec := &apiv1.ContainerRegistrySpec{
		Credentials: []apiv1.ContainerRegistryCredential{
			{
				Registry: "reg.example.com",
				Basic: &apiv1.ContainerRegistryBasicAuth{
					UsernameSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "basic"}, Key: "username"},
					PasswordSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "basic"}, Key: "password"},
				},
			},
			{
				Registry: "reg.example.com",
				Auth: &apiv1.ContainerRegistryAuth{
					AuthSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "auth"},
				},
			},
			{
				Registry: "reg.example.com",
				IdentityToken: &apiv1.ContainerRegistryIdentityToken{
					IdentityTokenSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "tok"}, Key: "identitytoken"},
				},
			},
		},
	}
	p := newResolveTestProvider(t, basic, auth, token)
	got, err := p.RegistryResolver().Resolve(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Credentials) != 3 {
		t.Fatalf("expected 3 credentials, got %d", len(got.Credentials))
	}
	if string(got.Credentials[0].Username) != "u" || string(got.Credentials[0].Password) != "p" {
		t.Errorf("basic credential = %+v", got.Credentials[0])
	}
	if string(got.Credentials[1].Auth) != "dXNlcjpwYXNz" {
		t.Errorf("auth credential = %+v", got.Credentials[1])
	}
	if string(got.Credentials[2].IdentityToken) != "oa-token" {
		t.Errorf("token credential = %+v", got.Credentials[2])
	}
}

func TestResolveContainerRegistry_CredentialMissingKey(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "auth", Namespace: "kube-system"},
		Data:       map[string][]byte{"other": []byte("v")},
	}
	spec := &apiv1.ContainerRegistrySpec{
		Credentials: []apiv1.ContainerRegistryCredential{
			{
				Registry: "reg.example.com",
				Auth: &apiv1.ContainerRegistryAuth{
					AuthSecretRef: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "auth"}, Key: "auth"},
				},
			},
		},
	}
	p := newResolveTestProvider(t, secret)
	_, err := p.RegistryResolver().Resolve(context.Background(), spec)
	if err == nil || !strings.Contains(err.Error(), "key") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}
