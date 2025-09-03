package bootstrap

import (
	"compress/gzip"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
)

func TestSKSBootstrap_Generate(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			ClusterDNS:      "10.96.0.10",
			ClusterDomain:   "cluster.local",
			BootstrapToken:  "abcdef.1234567890abcdef",
			CABundle:        []byte("test-ca-cert"),
			Labels: map[string]string{
				"node-type": "worker",
			},
		}

		nodeClass := &apiv1.ExoscaleNodeClass{
			Spec: apiv1.ExoscaleNodeClassSpec{
				KubeReserved: apiv1.ResourceReservation{
					CPU:    "100m",
					Memory: "512Mi",
				},
			},
		}

		result, err := bootstrap.Generate(options, nodeClass)

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		// Decode and decompress
		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		userData, err := io.ReadAll(gzReader)
		require.NoError(t, err)

		var config Config
		err = toml.Unmarshal(userData, &config)
		require.NoError(t, err)

		assert.Equal(t, "https://test-api.example.com", config.Settings.Kubernetes.APIServer)
		assert.Equal(t, "abcdef.1234567890abcdef", config.Settings.Kubernetes.BootstrapToken)
		assert.Equal(t, "external", config.Settings.Kubernetes.CloudProvider)
		assert.Equal(t, []string{"10.96.0.10"}, config.Settings.Kubernetes.ClusterDNSIP)
		assert.Equal(t, "cluster.local", config.Settings.Kubernetes.ClusterDomain)
		assert.Equal(t, "worker", config.Settings.Kubernetes.NodeLabels["node-type"])
		assert.Equal(t, "100m", config.Settings.Kubernetes.KubeReserved.CPU)
		assert.Equal(t, "512Mi", config.Settings.Kubernetes.KubeReserved.Memory)
	})

	t.Run("MultipleDNSIPs", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			ClusterDNS:      "10.96.0.10,10.96.0.11",
			ClusterDomain:   "cluster.local",
			BootstrapToken:  "abcdef.1234567890abcdef",
			CABundle:        []byte("test-ca-cert"),
		}

		result, err := bootstrap.Generate(options, nil)
		require.NoError(t, err)

		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		userData, err := io.ReadAll(gzReader)
		require.NoError(t, err)

		var config Config
		err = toml.Unmarshal(userData, &config)
		require.NoError(t, err)

		assert.Equal(t, []string{"10.96.0.10", "10.96.0.11"}, config.Settings.Kubernetes.ClusterDNSIP)
	})

	t.Run("WithTaints", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			ClusterDNS:      "10.96.0.10",
			ClusterDomain:   "cluster.local",
			BootstrapToken:  "abcdef.1234567890abcdef",
			CABundle:        []byte("test-ca-cert"),
			Taints: []v1.Taint{
				{
					Key:    "gpu",
					Value:  "true",
					Effect: v1.TaintEffectNoSchedule,
				},
			},
		}

		result, err := bootstrap.Generate(options, nil)
		require.NoError(t, err)

		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		userData, err := io.ReadAll(gzReader)
		require.NoError(t, err)

		var config Config
		err = toml.Unmarshal(userData, &config)
		require.NoError(t, err)

		assert.Equal(t, []string{"true:NoSchedule"}, config.Settings.Kubernetes.NodeTaints["gpu"])
	})

	t.Run("PreventsDuplicateTaints", func(t *testing.T) {
		bootstrap := New()

		// Create a NodeClass with GPU taint
		nodeClass := &apiv1.ExoscaleNodeClass{
			Spec: apiv1.ExoscaleNodeClassSpec{
				NodeTaints: []apiv1.NodeTaint{
					{
						Key:    "nvidia.com/gpu",
						Value:  "true",
						Effect: "NoSchedule",
					},
				},
			},
		}

		// Options also has the same GPU taint
		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			ClusterDNS:      "10.96.0.10",
			ClusterDomain:   "cluster.local",
			BootstrapToken:  "abcdef.1234567890abcdef",
			CABundle:        []byte("test-ca-cert"),
			Taints: []v1.Taint{
				{
					Key:    "nvidia.com/gpu",
					Value:  "true",
					Effect: v1.TaintEffectNoSchedule,
				},
			},
		}

		result, err := bootstrap.Generate(options, nodeClass)
		require.NoError(t, err)

		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		userData, err := io.ReadAll(gzReader)
		require.NoError(t, err)

		var config Config
		err = toml.Unmarshal(userData, &config)
		require.NoError(t, err)

		// Should only have one instance of the taint, not duplicated
		assert.Equal(t, []string{"true:NoSchedule"}, config.Settings.Kubernetes.NodeTaints["nvidia.com/gpu"])
		assert.Len(t, config.Settings.Kubernetes.NodeTaints["nvidia.com/gpu"], 1)
	})

	t.Run("MissingBootstrapToken", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			ClusterDNS:      "10.96.0.10",
			ClusterDomain:   "cluster.local",
			CABundle:        []byte("test-ca-cert"),
		}

		_, err := bootstrap.Generate(options, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap token is required")
	})

	t.Run("MissingCABundle", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			ClusterDNS:      "10.96.0.10",
			ClusterDomain:   "cluster.local",
			BootstrapToken:  "abcdef.1234567890abcdef",
		}

		_, err := bootstrap.Generate(options, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CA bundle is required")
	})
}
