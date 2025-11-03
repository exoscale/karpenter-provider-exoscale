package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"strings"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
	"github.com/pelletier/go-toml/v2"
	v1 "k8s.io/api/core/v1"
)

type Options struct {
	ClusterEndpoint             string
	ClusterDNS                  string
	ClusterDomain               string
	BootstrapToken              string
	CABundle                    []byte
	Taints                      []v1.Taint
	Labels                      map[string]string
	ImageGCHighThresholdPercent *int32
	ImageGCLowThresholdPercent  *int32
	ImageMinimumGCAge           string
}

type KubernetesSettings struct {
	APIServer                   string               `toml:"api-server"`
	BootstrapToken              string               `toml:"bootstrap-token"`
	CloudProvider               string               `toml:"cloud-provider"`
	ClusterCertificate          string               `toml:"cluster-certificate"`
	ClusterDNSIP                []string             `toml:"cluster-dns-ip,omitempty"`
	ClusterDomain               string               `toml:"cluster-domain,omitempty"`
	ImageGCHighThresholdPercent *int32               `toml:"image-gc-high-threshold-percent,omitempty"`
	ImageGCLowThresholdPercent  *int32               `toml:"image-gc-low-threshold-percent,omitempty"`
	ImageMinimumGCAge           string               `toml:"image-minimum-gc-age,omitempty"`
	KubeReserved                *ResourceReservation `toml:"kube-reserved,omitempty"`
	SystemReserved              *ResourceReservation `toml:"system-reserved,omitempty"`
	NodeTaints                  map[string][]string  `toml:"node-taints,omitempty"`
	NodeLabels                  map[string]string    `toml:"node-labels,omitempty"`
}

type ResourceReservation struct {
	CPU              string `toml:"cpu,omitempty"`
	Memory           string `toml:"memory,omitempty"`
	EphemeralStorage string `toml:"ephemeral-storage,omitempty"`
}

type Settings struct {
	Kubernetes KubernetesSettings `toml:"kubernetes"`
}

type Config struct {
	Settings Settings `toml:"settings"`
}

type SKSBootstrap struct{}

func New() *SKSBootstrap {
	return &SKSBootstrap{}
}

func (s *SKSBootstrap) Generate(options *Options, nodeClass *apiv1.ExoscaleNodeClass) (string, error) {
	if options == nil {
		return "", fmt.Errorf("options cannot be nil")
	}
	if options.BootstrapToken == "" {
		return "", fmt.Errorf("bootstrap token is required")
	}
	if len(options.CABundle) == 0 {
		return "", fmt.Errorf("CA bundle is required")
	}

	config := s.buildConfig(options, nodeClass)

	userData, err := s.marshalTOML(config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal user data to TOML: %w", err)
	}

	encodedUserData, err := s.compressAndEncode(userData)
	if err != nil {
		return "", fmt.Errorf("failed to compress and encode user data: %w", err)
	}

	return encodedUserData, nil
}

func (s *SKSBootstrap) buildConfig(options *Options, nodeClass *apiv1.ExoscaleNodeClass) *Config {
	config := &Config{
		Settings: Settings{
			Kubernetes: KubernetesSettings{
				APIServer:          options.ClusterEndpoint,
				BootstrapToken:     options.BootstrapToken,
				CloudProvider:      "external",
				ClusterCertificate: base64.StdEncoding.EncodeToString(options.CABundle),
			},
		},
	}

	if options.ClusterDNS != "" {
		ips := strings.Split(options.ClusterDNS, ",")
		for i, ip := range ips {
			ips[i] = strings.TrimSpace(ip)
		}
		config.Settings.Kubernetes.ClusterDNSIP = ips
	}

	if options.ClusterDomain != "" {
		config.Settings.Kubernetes.ClusterDomain = options.ClusterDomain
	}

	if nodeClass != nil {
		if nodeClass.Spec.ImageGCHighThresholdPercent != nil {
			config.Settings.Kubernetes.ImageGCHighThresholdPercent = nodeClass.Spec.ImageGCHighThresholdPercent
		}
		if nodeClass.Spec.ImageGCLowThresholdPercent != nil {
			config.Settings.Kubernetes.ImageGCLowThresholdPercent = nodeClass.Spec.ImageGCLowThresholdPercent
		}
		if nodeClass.Spec.ImageMinimumGCAge != "" {
			config.Settings.Kubernetes.ImageMinimumGCAge = nodeClass.Spec.ImageMinimumGCAge
		}

		if !isEmptyResourceReservation(nodeClass.Spec.KubeReserved) {
			config.Settings.Kubernetes.KubeReserved = convertResourceReservation(nodeClass.Spec.KubeReserved)
		}
		if !isEmptyResourceReservation(nodeClass.Spec.SystemReserved) {
			config.Settings.Kubernetes.SystemReserved = convertResourceReservation(nodeClass.Spec.SystemReserved)
		}

		if len(nodeClass.Spec.NodeLabels) > 0 {
			config.Settings.Kubernetes.NodeLabels = nodeClass.Spec.NodeLabels
		}
	}

	if options.ImageGCHighThresholdPercent != nil {
		config.Settings.Kubernetes.ImageGCHighThresholdPercent = options.ImageGCHighThresholdPercent
	}
	if options.ImageGCLowThresholdPercent != nil {
		config.Settings.Kubernetes.ImageGCLowThresholdPercent = options.ImageGCLowThresholdPercent
	}
	if options.ImageMinimumGCAge != "" {
		config.Settings.Kubernetes.ImageMinimumGCAge = options.ImageMinimumGCAge
	}

	if len(options.Taints) > 0 {
		if config.Settings.Kubernetes.NodeTaints == nil {
			config.Settings.Kubernetes.NodeTaints = make(map[string][]string)
		}
		for _, taint := range options.Taints {
			// IMPORTANT: sks-node-agent requires ALL taints to have a non-empty value
			// For taints that are semantically empty (like karpenter.sh/unregistered),
			// we use "true" as the value for compatibility
			value := taint.Value
			if value == "" {
				value = "true"
			}
			taintString := fmt.Sprintf("%s:%s", value, string(taint.Effect))
			// Check if this taint already exists to avoid duplicates
			exists := false
			for _, existing := range config.Settings.Kubernetes.NodeTaints[taint.Key] {
				if existing == taintString {
					exists = true
					break
				}
			}
			if !exists {
				config.Settings.Kubernetes.NodeTaints[taint.Key] = append(config.Settings.Kubernetes.NodeTaints[taint.Key], taintString)
			}
		}
	}

	if len(options.Labels) > 0 {
		if config.Settings.Kubernetes.NodeLabels == nil {
			config.Settings.Kubernetes.NodeLabels = make(map[string]string)
		}
		for k, v := range options.Labels {
			config.Settings.Kubernetes.NodeLabels[k] = v
		}
	}

	return config
}

func isEmptyResourceReservation(rr apiv1.ResourceReservation) bool {
	return rr.CPU == "" && rr.Memory == "" && rr.EphemeralStorage == ""
}

func convertResourceReservation(rr apiv1.ResourceReservation) *ResourceReservation {
	result := &ResourceReservation{}
	if rr.CPU != "" {
		result.CPU = rr.CPU
	}
	if rr.Memory != "" {
		result.Memory = rr.Memory
	}
	if rr.EphemeralStorage != "" {
		result.EphemeralStorage = rr.EphemeralStorage
	}
	return result
}

func (s *SKSBootstrap) marshalTOML(config *Config) ([]byte, error) {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	encoder.SetIndentTables(false)

	if err := encoder.Encode(config); err != nil {
		return nil, fmt.Errorf("failed to encode config to TOML: %w", err)
	}

	return buf.Bytes(), nil
}

func (s *SKSBootstrap) compressAndEncode(userData []byte) (string, error) {
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)

	if _, err := gzWriter.Write(userData); err != nil {
		return "", fmt.Errorf("failed to compress user data: %w", err)
	}

	if err := gzWriter.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
