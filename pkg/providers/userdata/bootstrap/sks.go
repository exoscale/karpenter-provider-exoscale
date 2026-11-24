package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"

	apiv1 "github.com/exoscale/karpenter-provider-exoscale/apis/karpenter/v1"
	"github.com/pelletier/go-toml/v2"
	v1 "k8s.io/api/core/v1"
)

type Options struct {
	ClusterEndpoint             string
	ClusterDNS                  []string
	ClusterDomain               string
	BootstrapToken              string
	CABundle                    []byte
	Taints                      []v1.Taint
	Labels                      map[string]string
	ImageGCHighThresholdPercent *int32
	ImageGCLowThresholdPercent  *int32
	ImageMinimumGCAge           string
	KubeReserved                apiv1.KubeResourceReservation
	SystemReserved              apiv1.SystemResourceReservation
	FeatureGates                map[string]bool
	UserData                    *string
	ContainerRegistry           *ContainerRegistrySettings
	CPUManagerPolicy            string
	CPUManagerPolicyOptions     []string
	CPUManagerReconcilePeriod   string
	MaxPods                     *int32
}

type ContainerRegistrySettings struct {
	Mirrors     []ContainerRegistryMirror             `toml:"mirrors,omitempty"`
	TLS         map[string]ContainerRegistryTLSConfig `toml:"tls,omitempty"`
	Credentials []ContainerRegistryCredentialConfig   `toml:"credentials,omitempty"`
}
type ContainerRegistryMirror struct {
	Registry string   `toml:"registry"`
	Endpoint []string `toml:"endpoint"`
}
type ContainerRegistryTLSConfig struct {
	CA           string `toml:"ca,omitempty"`
	Cert         string `toml:"cert,omitempty"`
	Key          string `toml:"key,omitempty"`
	OverridePath bool   `toml:"override_path,omitempty"`
	SkipVerify   bool   `toml:"skip_verify,omitempty"`
}
type ContainerRegistryCredentialConfig struct {
	Registry      string `toml:"registry"`
	Username      string `toml:"username,omitempty"`
	Password      string `toml:"password,omitempty"`
	Auth          string `toml:"auth,omitempty"`
	IdentityToken string `toml:"identitytoken,omitempty"`
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
	FeatureGates                map[string]bool      `toml:"feature-gates,omitempty"`
	CPUManagerPolicy            string               `toml:"cpu-manager-policy,omitempty"`
	CPUManagerPolicyOptions     []string             `toml:"cpu-manager-policy-options,omitempty"`
	CPUManagerReconcilePeriod   string               `toml:"cpu-manager-reconcile-period,omitempty"`
	MaxPods                     *int32               `toml:"max-pods,omitempty"`
}
type ResourceReservation struct {
	CPU              string `toml:"cpu,omitempty"`
	Memory           string `toml:"memory,omitempty"`
	EphemeralStorage string `toml:"ephemeral-storage,omitempty"`
}

type Settings struct {
	Kubernetes        KubernetesSettings         `toml:"kubernetes"`
	ContainerRegistry *ContainerRegistrySettings `toml:"container-registry,omitempty"`
}

type Config struct {
	Settings Settings `toml:"settings"`
}

func Generate(options *Options) (string, error) {
	if options.BootstrapToken == "" {
		return "", fmt.Errorf("bootstrap token is required")
	}
	if len(options.CABundle) == 0 {
		return "", fmt.Errorf("CA bundle is required")
	}

	config := buildConfig(options)

	var userData []byte
	var err error
	if options.UserData != nil && *options.UserData != "" {
		userData, err = mergeUserData(config, *options.UserData)
	} else {
		userData, err = marshalTOML(config)
	}
	if err != nil {
		return "", err
	}

	return compressAndEncode(userData)
}

// mergeUserData deep-merges user-provided TOML with the Karpenter-generated config.
// The generated config always takes precedence for Karpenter-managed fields.
func mergeUserData(config *Config, rawUserData string) ([]byte, error) {
	generatedTOML, err := marshalTOML(config)
	if err != nil {
		return nil, err
	}
	var generatedMap map[string]any
	if err := toml.Unmarshal(generatedTOML, &generatedMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal generated config: %w", err)
	}

	var userMap map[string]any
	if err := toml.Unmarshal([]byte(rawUserData), &userMap); err != nil {
		return nil, fmt.Errorf("failed to parse user data as TOML: %w", err)
	}

	return marshalTOML(deepMerge(userMap, generatedMap))
}

// deepMerge recursively merges src into dst. Values from src take precedence.
// For nested maps, merging is recursive. For all other types, src overwrites dst.
func deepMerge(dst, src map[string]any) map[string]any {
	result := make(map[string]any, len(dst))
	for k, v := range dst {
		result[k] = v
	}
	for k, v := range src {
		if srcMap, ok := v.(map[string]any); ok {
			if dstMap, ok := result[k].(map[string]any); ok {
				result[k] = deepMerge(dstMap, srcMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

func buildConfig(options *Options) *Config {
	k := KubernetesSettings{
		APIServer:                   options.ClusterEndpoint,
		BootstrapToken:              options.BootstrapToken,
		CloudProvider:               "external",
		ClusterCertificate:          base64.StdEncoding.EncodeToString(options.CABundle),
		ClusterDomain:               options.ClusterDomain,
		ImageGCHighThresholdPercent: options.ImageGCHighThresholdPercent,
		ImageGCLowThresholdPercent:  options.ImageGCLowThresholdPercent,
		ImageMinimumGCAge:           options.ImageMinimumGCAge,
		NodeLabels:                  maps.Clone(options.Labels),
		FeatureGates:                options.FeatureGates,
		CPUManagerPolicy:            options.CPUManagerPolicy,
		// nil leaves the kubelet built-in default in place.
		MaxPods: options.MaxPods,
	}

	if len(options.ClusterDNS) > 0 {
		k.ClusterDNSIP = make([]string, len(options.ClusterDNS))
		for i, ip := range options.ClusterDNS {
			k.ClusterDNSIP[i] = strings.TrimSpace(ip)
		}
	}

	if kr := options.KubeReserved; kr.CPU != "" || kr.Memory != "" || kr.EphemeralStorage != "" {
		k.KubeReserved = &ResourceReservation{CPU: kr.CPU, Memory: kr.Memory, EphemeralStorage: kr.EphemeralStorage}
	}
	if sr := options.SystemReserved; sr.CPU != "" || sr.Memory != "" || sr.EphemeralStorage != "" {
		k.SystemReserved = &ResourceReservation{CPU: sr.CPU, Memory: sr.Memory, EphemeralStorage: sr.EphemeralStorage}
	}

	if len(options.Taints) > 0 {
		k.NodeTaints = make(map[string][]string)
		for _, taint := range options.Taints {
			// sks-node-agent requires every taint to have a non-empty value, so
			// semantically empty taints (like karpenter.sh/unregistered) get "true".
			value := taint.Value
			if value == "" {
				value = "true"
			}
			taintString := value + ":" + string(taint.Effect)
			if !slices.Contains(k.NodeTaints[taint.Key], taintString) {
				k.NodeTaints[taint.Key] = append(k.NodeTaints[taint.Key], taintString)
			}
		}
	}

	if options.CPUManagerPolicy == "static" && len(options.CPUManagerPolicyOptions) > 0 {
		k.CPUManagerPolicyOptions = options.CPUManagerPolicyOptions
	}
	// Skip emission when the value equals the kubelet default; the apiserver
	// injects the default server-side on admission, so an unset spec field
	// would otherwise be redundantly written into user-data.
	if options.CPUManagerReconcilePeriod != apiv1.DefaultCPUManagerReconcilePeriod {
		k.CPUManagerReconcilePeriod = options.CPUManagerReconcilePeriod
	}

	return &Config{Settings: Settings{Kubernetes: k, ContainerRegistry: options.ContainerRegistry}}
}

func marshalTOML(v any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := toml.NewEncoder(&buf)
	encoder.SetIndentTables(false)
	if err := encoder.Encode(v); err != nil {
		return nil, fmt.Errorf("failed to encode user data to TOML: %w", err)
	}
	return buf.Bytes(), nil
}

func compressAndEncode(userData []byte) (string, error) {
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
