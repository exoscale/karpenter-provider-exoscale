package bootstrap

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"strings"
	"text/template"

	apiv1 "github.com/exoscale/karpenter-exoscale/apis/karpenter/v1"
)

type Options struct {
	ClusterName                 string
	ClusterEndpoint             string
	ClusterDNS                  string
	ClusterDomain               string
	BootstrapToken              string
	CABundle                    []byte
	Taints                      []apiv1.NodeTaint
	Labels                      map[string]string
	ImageGCHighThresholdPercent *int32
	ImageGCLowThresholdPercent  *int32
	ImageMinimumGCAge           string
	KubeletMaxPods              *int32
}

type SKSBootstrap struct{}

const SKSUserDataTemplate = `[settings.kubernetes]
api-server = "{{ .APIServer }}"
bootstrap-token = "{{ .BootstrapToken }}"
cloud-provider = "{{ .CloudProvider }}"
cluster-certificate = "{{ .ClusterCertificate }}"
{{- if .ClusterDNSIP }}
cluster-dns-ip = {{ .ClusterDNSIP }}
{{- end }}
{{- if .ClusterDomain }}
cluster-domain = "{{ .ClusterDomain }}"
{{- end }}
{{- if .ImageGCHighThresholdPercent }}
image-gc-high-threshold-percent = {{ .ImageGCHighThresholdPercent }}
{{- end }}
{{- if .ImageGCLowThresholdPercent }}
image-gc-low-threshold-percent = {{ .ImageGCLowThresholdPercent }}
{{- end }}
{{- if .ImageMinimumGCAge }}
image-minimum-gc-age = "{{ .ImageMinimumGCAge }}"
{{- end }}
{{- if .KubeReserved }}

[settings.kubernetes.kube-reserved]
{{- if .KubeReserved.CPU }}
cpu = "{{ .KubeReserved.CPU }}"
{{- end }}
{{- if .KubeReserved.Memory }}
memory = "{{ .KubeReserved.Memory }}"
{{- end }}
{{- if .KubeReserved.EphemeralStorage }}
ephemeral-storage = "{{ .KubeReserved.EphemeralStorage }}"
{{- end }}
{{- end }}
{{- if .SystemReserved }}

[settings.kubernetes.system-reserved]
{{- if .SystemReserved.CPU }}
cpu = "{{ .SystemReserved.CPU }}"
{{- end }}
{{- if .SystemReserved.Memory }}
memory = "{{ .SystemReserved.Memory }}"
{{- end }}
{{- if .SystemReserved.EphemeralStorage }}
ephemeral-storage = "{{ .SystemReserved.EphemeralStorage }}"
{{- end }}
{{- end }}
{{- if .EvictionHard }}

[settings.kubernetes.eviction-hard]
{{- if .EvictionHard.MemoryAvailable }}
memory-available = "{{ .EvictionHard.MemoryAvailable }}"
{{- end }}
{{- if .EvictionHard.NodeFSAvailable }}
nodefs-available = "{{ .EvictionHard.NodeFSAvailable }}"
{{- end }}
{{- if .EvictionHard.NodeFSInodesFree }}
nodefs-inodes-free = "{{ .EvictionHard.NodeFSInodesFree }}"
{{- end }}
{{- if .EvictionHard.ImageFSAvailable }}
imagefs-available = "{{ .EvictionHard.ImageFSAvailable }}"
{{- end }}
{{- if .EvictionHard.ImageFSInodesFree }}
imagefs-inodes-free = "{{ .EvictionHard.ImageFSInodesFree }}"
{{- end }}
{{- if .EvictionHard.PIDAvailable }}
pid-available = "{{ .EvictionHard.PIDAvailable }}"
{{- end }}
{{- end }}
{{- if .KubeletMaxPods }}

[settings.kubernetes.kubelet]
max-pods = {{ .KubeletMaxPods }}
{{- end }}
{{- if .Taints }}

[settings.kubernetes.taints]
{{- range $index, $taint := .Taints }}
{{ $index }} = "{{ $taint.Key }}={{ $taint.Value }}:{{ $taint.Effect }}"
{{- end }}
{{- end }}
{{- if .Labels }}

[settings.kubernetes.labels]
{{- range $key, $value := .Labels }}
"{{ $key }}" = "{{ $value }}"
{{- end }}
{{- end }}`

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

	templateData := s.buildTemplateData(options, nodeClass)

	userData, err := s.renderTemplate(templateData)
	if err != nil {
		return "", fmt.Errorf("failed to render user data template: %w", err)
	}

	encodedUserData, err := s.compressAndEncode(userData)
	if err != nil {
		return "", fmt.Errorf("failed to compress and encode user data: %w", err)
	}

	return encodedUserData, nil
}

func (s *SKSBootstrap) buildTemplateData(options *Options, nodeClass *apiv1.ExoscaleNodeClass) interface{} {

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
		APIServer:          options.ClusterEndpoint,
		BootstrapToken:     options.BootstrapToken,
		CloudProvider:      "external",
		ClusterCertificate: base64.StdEncoding.EncodeToString(options.CABundle),
		ClusterDNSIP:       s.formatClusterDNSIP(options.ClusterDNS),
		ClusterDomain:      options.ClusterDomain,
		Taints:             options.Taints,
		Labels:             options.Labels,
	}

	if nodeClass != nil {
		data.ImageGCHighThresholdPercent = nodeClass.Spec.ImageGCHighThresholdPercent
		data.ImageGCLowThresholdPercent = nodeClass.Spec.ImageGCLowThresholdPercent
		data.ImageMinimumGCAge = nodeClass.Spec.ImageMinimumGCAge
		data.KubeReserved = nodeClass.Spec.KubeReserved
		data.SystemReserved = nodeClass.Spec.SystemReserved

		if len(nodeClass.Spec.NodeTaints) > 0 {
			data.Taints = nodeClass.Spec.NodeTaints
		}

		if len(nodeClass.Spec.NodeLabels) > 0 {
			if data.Labels == nil {
				data.Labels = make(map[string]string)
			}
			for k, v := range nodeClass.Spec.NodeLabels {
				data.Labels[k] = v
			}
		}
	}

	if options.ImageGCHighThresholdPercent != nil {
		data.ImageGCHighThresholdPercent = options.ImageGCHighThresholdPercent
	}
	if options.ImageGCLowThresholdPercent != nil {
		data.ImageGCLowThresholdPercent = options.ImageGCLowThresholdPercent
	}
	if options.ImageMinimumGCAge != "" {
		data.ImageMinimumGCAge = options.ImageMinimumGCAge
	}
	if options.KubeletMaxPods != nil {
		data.KubeletMaxPods = *options.KubeletMaxPods
	}

	return data
}

func (s *SKSBootstrap) formatClusterDNSIP(clusterDNS string) interface{} {
	if clusterDNS == "" {
		return nil
	}

	ips := strings.Split(clusterDNS, ",")
	if len(ips) == 1 {
		return fmt.Sprintf(`"%s"`, strings.TrimSpace(ips[0]))
	}

	var formatted []string
	for _, ip := range ips {
		formatted = append(formatted, fmt.Sprintf(`"%s"`, strings.TrimSpace(ip)))
	}
	return fmt.Sprintf("[%s]", strings.Join(formatted, ", "))
}

func (s *SKSBootstrap) renderTemplate(data interface{}) ([]byte, error) {
	tmpl, err := template.New("userdata").Parse(SKSUserDataTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse user data template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("failed to execute user data template: %w", err)
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
