package bootstrap

import (
	"compress/gzip"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSKSBootstrap_Generate(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterName:     "test-cluster",
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

		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		var userData strings.Builder
		_, err = io.Copy(&userData, gzReader)
		require.NoError(t, err)

		userDataContent := userData.String()
		assert.Contains(t, userDataContent, `api-server = "https://test-api.example.com"`)
		assert.Contains(t, userDataContent, `bootstrap-token = "abcdef.1234567890abcdef"`)
		assert.Contains(t, userDataContent, `cluster-dns-ip = "10.96.0.10"`)
		assert.Contains(t, userDataContent, `cluster-domain = "cluster.local"`)
		assert.Contains(t, userDataContent, `cpu = "100m"`)
		assert.Contains(t, userDataContent, `memory = "512Mi"`)
		assert.Contains(t, userDataContent, `"node-type" = "worker"`)
	})

	t.Run("MissingBootstrapToken", func(t *testing.T) {
		bootstrap := New()

		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
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
			BootstrapToken:  "abcdef.1234567890abcdef",
		}

		_, err := bootstrap.Generate(options, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "CA bundle is required")
	})

	t.Run("NilOptions", func(t *testing.T) {
		bootstrap := New()

		_, err := bootstrap.Generate(nil, nil)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "options cannot be nil")
	})
}

func TestSKSBootstrap_FormatClusterDNSIP(t *testing.T) {
	bootstrap := New()

	t.Run("SingleIP", func(t *testing.T) {
		result := bootstrap.formatClusterDNSIP("10.96.0.10")
		assert.Equal(t, `"10.96.0.10"`, result)
	})

	t.Run("MultipleIPs", func(t *testing.T) {
		result := bootstrap.formatClusterDNSIP("10.96.0.10,10.96.0.11")
		assert.Equal(t, `["10.96.0.10", "10.96.0.11"]`, result)
	})

	t.Run("EmptyString", func(t *testing.T) {
		result := bootstrap.formatClusterDNSIP("")
		assert.Nil(t, result)
	})

	t.Run("IPsWithSpaces", func(t *testing.T) {
		result := bootstrap.formatClusterDNSIP(" 10.96.0.10 , 10.96.0.11 ")
		assert.Equal(t, `["10.96.0.10", "10.96.0.11"]`, result)
	})
}

func TestSKSBootstrap_BuildTemplateData(t *testing.T) {
	bootstrap := New()

	t.Run("WithNodeClass", func(t *testing.T) {
		options := &Options{
			ClusterEndpoint: "https://test-api.example.com",
			BootstrapToken:  "abcdef.1234567890abcdef",
			CABundle:        []byte("test-ca-cert"),
			ClusterDNS:      "10.96.0.10",
			ClusterDomain:   "cluster.local",
			Labels: map[string]string{
				"from-options": "value1",
			},
		}

		nodeClass := &apiv1.ExoscaleNodeClass{
			Spec: apiv1.ExoscaleNodeClassSpec{
				KubeReserved: apiv1.ResourceReservation{
					CPU:    "100m",
					Memory: "512Mi",
				},
				NodeLabels: map[string]string{
					"from-nodeclass": "value2",
				},
				NodeTaints: []apiv1.NodeTaint{
					{
						Key:    "test-taint",
						Value:  "test-value",
						Effect: "NoSchedule",
					},
				},
			},
		}

		data := bootstrap.buildTemplateData(options, nodeClass)

		templateData, ok := data.(struct {
			APIServer                   string
			BootstrapToken              string
			CloudProvider               string
			ClusterCertificate          string
			ClusterDNSIP                interface{}
			ClusterDomain               string
			ImageGCHighThresholdPercent *int32
			ImageGCLowThresholdPercent  *int32
			ImageMinimumGCAge           string
			KubeReserved                apiv1.ResourceReservation
			SystemReserved              apiv1.ResourceReservation
			EvictionHard                interface{}
			KubeletMaxPods              interface{}
			Taints                      []apiv1.NodeTaint
			Labels                      map[string]string
		})
		require.True(t, ok)

		assert.Equal(t, "https://test-api.example.com", templateData.APIServer)
		assert.Equal(t, "abcdef.1234567890abcdef", templateData.BootstrapToken)
		assert.Equal(t, "external", templateData.CloudProvider)
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("test-ca-cert")), templateData.ClusterCertificate)
		assert.Equal(t, `"10.96.0.10"`, templateData.ClusterDNSIP)
		assert.Equal(t, "cluster.local", templateData.ClusterDomain)
		assert.Equal(t, "100m", templateData.KubeReserved.CPU)
		assert.Equal(t, "512Mi", templateData.KubeReserved.Memory)
		assert.Len(t, templateData.Taints, 1)
		assert.Equal(t, "test-taint", templateData.Taints[0].Key)
		assert.Len(t, templateData.Labels, 2)
		assert.Equal(t, "value1", templateData.Labels["from-options"])
		assert.Equal(t, "value2", templateData.Labels["from-nodeclass"])
	})

	t.Run("OptionsOverrideNodeClass", func(t *testing.T) {
		gcHigh := int32(85)
		gcLow := int32(80)
		maxPods := int32(110)

		options := &Options{
			ClusterEndpoint:             "https://test-api.example.com",
			BootstrapToken:              "abcdef.1234567890abcdef",
			CABundle:                    []byte("test-ca-cert"),
			ImageGCHighThresholdPercent: &gcHigh,
			ImageGCLowThresholdPercent:  &gcLow,
			ImageMinimumGCAge:           "24h",
			KubeletMaxPods:              &maxPods,
		}

		nodeClassGcHigh := int32(90)
		nodeClass := &apiv1.ExoscaleNodeClass{
			Spec: apiv1.ExoscaleNodeClassSpec{
				ImageGCHighThresholdPercent: &nodeClassGcHigh,
			},
		}

		data := bootstrap.buildTemplateData(options, nodeClass)

		templateData, ok := data.(struct {
			APIServer                   string
			BootstrapToken              string
			CloudProvider               string
			ClusterCertificate          string
			ClusterDNSIP                interface{}
			ClusterDomain               string
			ImageGCHighThresholdPercent *int32
			ImageGCLowThresholdPercent  *int32
			ImageMinimumGCAge           string
			KubeReserved                apiv1.ResourceReservation
			SystemReserved              apiv1.ResourceReservation
			EvictionHard                interface{}
			KubeletMaxPods              interface{}
			Taints                      []apiv1.NodeTaint
			Labels                      map[string]string
		})
		require.True(t, ok)

		assert.Equal(t, int32(85), *templateData.ImageGCHighThresholdPercent)
		assert.Equal(t, int32(80), *templateData.ImageGCLowThresholdPercent)
		assert.Equal(t, "24h", templateData.ImageMinimumGCAge)
		assert.Equal(t, int32(110), templateData.KubeletMaxPods)
	})
}

func TestSKSBootstrap_RenderTemplate(t *testing.T) {
	bootstrap := New()

	t.Run("FullTemplate", func(t *testing.T) {
		gcHigh := int32(85)
		maxPods := int32(110)

		data := struct {
			APIServer                   string
			BootstrapToken              string
			CloudProvider               string
			ClusterCertificate          string
			ClusterDNSIP                interface{}
			ClusterDomain               string
			ImageGCHighThresholdPercent *int32
			ImageGCLowThresholdPercent  *int32
			ImageMinimumGCAge           string
			KubeReserved                apiv1.ResourceReservation
			SystemReserved              apiv1.ResourceReservation
			EvictionHard                interface{}
			KubeletMaxPods              interface{}
			Taints                      []apiv1.NodeTaint
			Labels                      map[string]string
		}{
			APIServer:                   "https://test-api.example.com",
			BootstrapToken:              "abcdef.1234567890abcdef",
			CloudProvider:               "external",
			ClusterCertificate:          "dGVzdC1jYS1jZXJ0",
			ClusterDNSIP:                `"10.96.0.10"`,
			ClusterDomain:               "cluster.local",
			ImageGCHighThresholdPercent: &gcHigh,
			KubeletMaxPods:              maxPods,
			KubeReserved: apiv1.ResourceReservation{
				CPU:    "100m",
				Memory: "512Mi",
			},
			Taints: []apiv1.NodeTaint{
				{Key: "test-taint", Value: "test-value", Effect: "NoSchedule"},
			},
			Labels: map[string]string{
				"node-type": "worker",
			},
		}

		result, err := bootstrap.renderTemplate(data)

		require.NoError(t, err)
		resultStr := string(result)

		assert.Contains(t, resultStr, `api-server = "https://test-api.example.com"`)
		assert.Contains(t, resultStr, `bootstrap-token = "abcdef.1234567890abcdef"`)
		assert.Contains(t, resultStr, `cluster-dns-ip = "10.96.0.10"`)
		assert.Contains(t, resultStr, `image-gc-high-threshold-percent = 85`)
		assert.Contains(t, resultStr, `max-pods = 110`)
		assert.Contains(t, resultStr, `cpu = "100m"`)
		assert.Contains(t, resultStr, `0 = "test-taint=test-value:NoSchedule"`)
		assert.Contains(t, resultStr, `"node-type" = "worker"`)
	})

	t.Run("MinimalTemplate", func(t *testing.T) {
		data := struct {
			APIServer                   string
			BootstrapToken              string
			CloudProvider               string
			ClusterCertificate          string
			ClusterDNSIP                interface{}
			ClusterDomain               string
			ImageGCHighThresholdPercent *int32
			ImageGCLowThresholdPercent  *int32
			ImageMinimumGCAge           string
			KubeReserved                apiv1.ResourceReservation
			SystemReserved              apiv1.ResourceReservation
			EvictionHard                interface{}
			KubeletMaxPods              interface{}
			Taints                      []apiv1.NodeTaint
			Labels                      map[string]string
		}{
			APIServer:          "https://minimal-api.example.com",
			BootstrapToken:     "minimal.token",
			CloudProvider:      "external",
			ClusterCertificate: "bWluaW1hbC1jZXJ0",
		}

		result, err := bootstrap.renderTemplate(data)

		require.NoError(t, err)
		resultStr := string(result)

		assert.Contains(t, resultStr, `api-server = "https://minimal-api.example.com"`)
		assert.Contains(t, resultStr, `bootstrap-token = "minimal.token"`)
		assert.NotContains(t, resultStr, "cluster-dns-ip")
		assert.NotContains(t, resultStr, "max-pods")
		assert.NotContains(t, resultStr, "taints")
		assert.NotContains(t, resultStr, "labels")
	})
}

func TestSKSBootstrap_CompressAndEncode(t *testing.T) {
	bootstrap := New()

	t.Run("Success", func(t *testing.T) {
		testData := []byte("test user data content")

		result, err := bootstrap.compressAndEncode(testData)

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		var output strings.Builder
		_, err = io.Copy(&output, gzReader)
		require.NoError(t, err)

		assert.Equal(t, "test user data content", output.String())
	})

	t.Run("EmptyData", func(t *testing.T) {
		testData := []byte("")

		result, err := bootstrap.compressAndEncode(testData)

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		decodedData, err := base64.StdEncoding.DecodeString(result)
		require.NoError(t, err)

		gzReader, err := gzip.NewReader(strings.NewReader(string(decodedData)))
		require.NoError(t, err)
		defer gzReader.Close()

		var output strings.Builder
		_, err = io.Copy(&output, gzReader)
		require.NoError(t, err)

		assert.Equal(t, "", output.String())
	})
}

func TestNew(t *testing.T) {
	bootstrap := New()
	assert.NotNil(t, bootstrap)
	assert.IsType(t, &SKSBootstrap{}, bootstrap)
}
