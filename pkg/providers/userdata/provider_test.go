package userdata

import (
	"context"
	"encoding/base64"
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

type test struct {
	Ctx    context.Context
	Client client.Client
}

func setup(t *testing.T, objects ...client.Object) *test {
	t.Helper()

	s := runtime.NewScheme()
	require.NoError(t, scheme.AddToScheme(s))

	gv := schema.GroupVersion{Group: "karpenter.sh", Version: "v1"}
	s.AddKnownTypes(gv, &karpenterv1.NodeClaim{})
	s.AddKnownTypes(apiv1.GroupVersion, &apiv1.ExoscaleNodeClass{})

	builder := fake.NewClientBuilder().WithScheme(s)
	if len(objects) > 0 {
		builder = builder.WithObjects(objects...)
	}

	return &test{
		Ctx:    context.Background(),
		Client: builder.Build(),
	}
}

func TestNewProvider(t *testing.T) {
	test := setup(t)

	provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

	assert.NotNil(t, provider)
	defaultProvider, ok := provider.(*DefaultProvider)
	require.True(t, ok)
	assert.Equal(t, "https://test-api.example.com", defaultProvider.clusterEndpoint)
	assert.Equal(t, "10.96.0.10", defaultProvider.clusterDNS)
	assert.Equal(t, "cluster.local", defaultProvider.clusterDomain)
	assert.NotNil(t, defaultProvider.sksBootstrap)
}

func TestDefaultProvider_Generate(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		clusterInfoCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-info",
				Namespace: "kube-public",
			},
			Data: map[string]string{
				"kubeconfig": `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString([]byte("test-ca-cert")) + `
    server: https://test-api.example.com
  name: test-cluster`,
			},
		}

		test := setup(t, clusterInfoCM)
		provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
			Spec: apiv1.ExoscaleNodeClassSpec{
				KubeReserved: apiv1.ResourceReservation{
					CPU:    "100m",
					Memory: "512Mi",
				},
			},
		}

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-nodeclaim",
				Labels: map[string]string{
					"node-type": "worker",
				},
			},
			Spec: karpenterv1.NodeClaimSpec{
				Taints: []corev1.Taint{
					{
						Key:    "test-taint",
						Value:  "test-value",
						Effect: corev1.TaintEffectNoSchedule,
					},
				},
			},
		}

		options := &Options{
			ClusterName:    "test-cluster",
			BootstrapToken: "abcdef.1234567890abcdef",
		}

		result, err := provider.Generate(test.Ctx, nodeClass, nodeClaim, options)

		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("WithProvidedCABundle", func(t *testing.T) {
		test := setup(t)
		provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
		}

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
		}

		options := &Options{
			ClusterName:    "test-cluster",
			BootstrapToken: "abcdef.1234567890abcdef",
			CABundle:       []byte("provided-ca-cert"),
		}

		result, err := provider.Generate(test.Ctx, nodeClass, nodeClaim, options)

		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("FallbackToKubeRootCA", func(t *testing.T) {
		kubeRootCACM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-root-ca.crt",
				Namespace: "kube-system",
			},
			Data: map[string]string{
				"ca.crt": "-----BEGIN CERTIFICATE-----\ntest-ca-cert\n-----END CERTIFICATE-----",
			},
		}

		test := setup(t, kubeRootCACM)
		provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
		}

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
		}

		options := &Options{
			ClusterName:    "test-cluster",
			BootstrapToken: "abcdef.1234567890abcdef",
		}

		result, err := provider.Generate(test.Ctx, nodeClass, nodeClaim, options)

		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("NilNodeClass", func(t *testing.T) {
		test := setup(t)
		provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
		}

		options := &Options{
			ClusterName:    "test-cluster",
			BootstrapToken: "abcdef.1234567890abcdef",
		}

		ctx := context.Background()
		_, err := provider.Generate(ctx, nil, nodeClaim, options)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "nodeClass cannot be nil")
	})

	t.Run("NilOptions", func(t *testing.T) {
		test := setup(t)
		provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
		}

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
		}

		ctx := context.Background()
		_, err := provider.Generate(ctx, nodeClass, nodeClaim, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "options cannot be nil")
	})

	t.Run("UseDefaultValues", func(t *testing.T) {
		env := setup(t)
		provider := NewProvider(env.Client, "https://default-api.example.com", "10.96.0.11", "default.local")

		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
		}

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
		}

		options := &Options{
			ClusterName:    "test-cluster",
			BootstrapToken: "abcdef.1234567890abcdef",
			CABundle:       []byte("test-ca-cert"),
		}

		result, err := provider.Generate(env.Ctx, nodeClass, nodeClaim, options)

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		assert.Equal(t, "https://default-api.example.com", options.ClusterEndpoint)
		assert.Equal(t, "10.96.0.11", options.ClusterDNS)
		assert.Equal(t, "default.local", options.ClusterDomain)
	})

	t.Run("NoCAAvailable", func(t *testing.T) {
		test := setup(t)
		provider := NewProvider(test.Client, "https://test-api.example.com", "10.96.0.10", "cluster.local")

		nodeClass := &apiv1.ExoscaleNodeClass{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclass"},
		}

		nodeClaim := &karpenterv1.NodeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: "test-nodeclaim"},
		}

		options := &Options{
			ClusterName:    "test-cluster",
			BootstrapToken: "abcdef.1234567890abcdef",
		}

		ctx := context.Background()
		_, err := provider.Generate(ctx, nodeClass, nodeClaim, options)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get cluster CA")
	})
}

func TestDefaultProvider_GetClusterCA(t *testing.T) {
	t.Run("FromClusterInfo", func(t *testing.T) {
		clusterInfoCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-info",
				Namespace: "kube-public",
			},
			Data: map[string]string{
				"kubeconfig": `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString([]byte("test-ca-cert")) + `
    server: https://test-api.example.com
  name: test-cluster`,
			},
		}

		test := setup(t, clusterInfoCM)
		provider := &DefaultProvider{kubeClient: test.Client}

		result, err := provider.getClusterCA(test.Ctx)

		require.NoError(t, err)
		assert.Equal(t, []byte("test-ca-cert"), result)
	})

	t.Run("FromKubeRootCA", func(t *testing.T) {
		kubeRootCACM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-root-ca.crt",
				Namespace: "kube-system",
			},
			Data: map[string]string{
				"ca.crt": "-----BEGIN CERTIFICATE-----\ntest-ca-cert\n-----END CERTIFICATE-----",
			},
		}

		test := setup(t, kubeRootCACM)
		provider := &DefaultProvider{kubeClient: test.Client}

		result, err := provider.getClusterCA(test.Ctx)

		require.NoError(t, err)
		assert.Equal(t, []byte("-----BEGIN CERTIFICATE-----\ntest-ca-cert\n-----END CERTIFICATE-----"), result)
	})

	t.Run("NoCAAvailable", func(t *testing.T) {
		test := setup(t)
		provider := &DefaultProvider{kubeClient: test.Client}

		_, err := provider.getClusterCA(test.Ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get kube-root-ca.crt ConfigMap")
	})

	t.Run("MissingCACrtInConfigMap", func(t *testing.T) {
		kubeRootCACM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-root-ca.crt",
				Namespace: "kube-system",
			},
			Data: map[string]string{
				"other-data": "not-ca-cert",
			},
		}

		test := setup(t, kubeRootCACM)
		provider := &DefaultProvider{kubeClient: test.Client}

		_, err := provider.getClusterCA(test.Ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "ca.crt not found in kube-root-ca.crt ConfigMap")
	})
}

func TestDefaultProvider_GetCACertFromClusterInfo(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		clusterInfoCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-info",
				Namespace: "kube-public",
			},
			Data: map[string]string{
				"kubeconfig": `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString([]byte("test-ca-cert")) + `
    server: https://test-api.example.com
  name: test-cluster`,
			},
		}

		test := setup(t, clusterInfoCM)
		provider := &DefaultProvider{kubeClient: test.Client}

		result, err := provider.getCACertFromClusterInfo(test.Ctx)

		require.NoError(t, err)
		assert.Equal(t, []byte("test-ca-cert"), result)
	})

	t.Run("ConfigMapNotFound", func(t *testing.T) {
		test := setup(t)
		provider := &DefaultProvider{kubeClient: test.Client}

		_, err := provider.getCACertFromClusterInfo(test.Ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cluster-info ConfigMap not found")
	})

	t.Run("MissingKubeconfig", func(t *testing.T) {
		clusterInfoCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-info",
				Namespace: "kube-public",
			},
			Data: map[string]string{
				"other-data": "not-kubeconfig",
			},
		}

		test := setup(t, clusterInfoCM)
		provider := &DefaultProvider{kubeClient: test.Client}

		_, err := provider.getCACertFromClusterInfo(test.Ctx)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "kubeconfig not found in cluster-info ConfigMap")
	})
}

func TestDefaultProvider_ExtractCACertFromKubeconfig(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		provider := &DefaultProvider{}
		kubeconfig := `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: ` + base64.StdEncoding.EncodeToString([]byte("test-ca-cert")) + `
    server: https://test-api.example.com
  name: test-cluster`

		result, err := provider.extractCACertFromKubeconfig(kubeconfig)

		require.NoError(t, err)
		assert.Equal(t, []byte("test-ca-cert"), result)
	})

	t.Run("InvalidYAML", func(t *testing.T) {
		provider := &DefaultProvider{}
		kubeconfig := `invalid: yaml: content: [unclosed`

		_, err := provider.extractCACertFromKubeconfig(kubeconfig)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal kubeconfig")
	})

	t.Run("NoClusters", func(t *testing.T) {
		provider := &DefaultProvider{}
		kubeconfig := `apiVersion: v1
clusters: []`

		_, err := provider.extractCACertFromKubeconfig(kubeconfig)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no clusters found in kubeconfig")
	})

	t.Run("MissingClusterData", func(t *testing.T) {
		provider := &DefaultProvider{}
		kubeconfig := `apiVersion: v1
clusters:
- name: test-cluster`

		_, err := provider.extractCACertFromKubeconfig(kubeconfig)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cluster data not found in kubeconfig")
	})

	t.Run("MissingCertificateAuthorityData", func(t *testing.T) {
		provider := &DefaultProvider{}
		kubeconfig := `apiVersion: v1
clusters:
- cluster:
    server: https://test-api.example.com
  name: test-cluster`

		_, err := provider.extractCACertFromKubeconfig(kubeconfig)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "certificate-authority-data not found in kubeconfig")
	})

	t.Run("InvalidBase64", func(t *testing.T) {
		provider := &DefaultProvider{}
		kubeconfig := `apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: invalid-base64!@#
    server: https://test-api.example.com
  name: test-cluster`

		_, err := provider.extractCACertFromKubeconfig(kubeconfig)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to decode CA certificate")
	})
}
