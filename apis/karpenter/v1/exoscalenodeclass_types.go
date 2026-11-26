// +groupName=karpenter.exoscale.com
package v1

import (
	"github.com/awslabs/operatorpkg/status"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

func init() {
	metav1.AddToGroupVersion(scheme.Scheme, GroupVersion)
	scheme.Scheme.AddKnownTypes(GroupVersion,
		&ExoscaleNodeClass{},
		&ExoscaleNodeClassList{})
}

// ExoscaleNodeClass describes the Exoscale instances Karpenter launches for a NodePool.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,categories={karpenter}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description=""
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description=""
// +kubebuilder:storageversion
type ExoscaleNodeClass struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   ExoscaleNodeClassSpec   `json:"spec"`
	Status ExoscaleNodeClassStatus `json:"status,omitempty"`
}

// ImageTemplateSelector picks the active SKS nodepool template.
type ImageTemplateSelector struct {
	// Version is the Kubernetes version of the template, e.g. 1.33.2.
	// Defaults to the cluster's version.
	// +optional
	// +kubebuilder:validation:Pattern="^[0-9]+\\.[0-9]+\\.[0-9]+$"
	Version string `json:"version,omitempty"`

	// Variant is the template flavour: standard, or nvidia for GPU instances.
	// +optional
	// +kubebuilder:default="standard"
	// +kubebuilder:validation:Enum=standard;nvidia
	Variant string `json:"variant,omitempty"`
}

// SelectorTerms selects an Exoscale resource by ID or by name, not both.
// +kubebuilder:validation:XValidation:rule="(has(self.id) && !has(self.name)) || (!has(self.id) && has(self.name))",message="exactly one of id or name must be specified"
type SelectorTerms struct {
	// +optional
	ID string `json:"id,omitempty"`
	// +optional
	Name string `json:"name,omitempty"`
}

// ExoscaleNodeClassSpec configures the instances launched from this NodeClass.
// +kubebuilder:validation:XValidation:rule="!has(self.kubelet) || !has(self.kubelet.imageGCHighThresholdPercent) || !has(self.kubelet.imageGCLowThresholdPercent) || self.kubelet.imageGCLowThresholdPercent < self.kubelet.imageGCHighThresholdPercent",message="imageGCLowThresholdPercent must be less than imageGCHighThresholdPercent"
// +kubebuilder:validation:XValidation:rule="(has(self.templateID) && !has(self.imageTemplateSelector)) || (!has(self.templateID) && has(self.imageTemplateSelector))",message="exactly one of templateID or imageTemplateSelector must be specified"
// +kubebuilder:validation:XValidation:rule="!has(self.containerRegistry) || (self.containerRegistry.mirrors.all(m, m.endpoints.all(e, !has(e.tlsSecretRef) || size(e.tlsSecretRef.name) > 0)) && (!has(self.containerRegistry.credentials) || self.containerRegistry.credentials.all(c, [has(c.basic), has(c.auth), has(c.identityToken)].filter(x, x).size() == 1)))",message="containerRegistry.mirrors[*].endpoints[*].tlsSecretRef.name must be non-empty when set, and each credentials entry must declare exactly one of basic/auth/identityToken"
// +kubebuilder:validation:XValidation:rule="!(has(self.userData) && size(self.userData) > 0 && has(self.containerRegistry))",message="userData and containerRegistry are mutually exclusive; configure container registry via the structured containerRegistry field only"
// +kubebuilder:validation:XValidation:rule="!has(self.vpcSubnetSelectorTerms) || size(self.vpcSubnetSelectorTerms) == 0 || has(self.vpcSelector)",message="vpcSelector is required when vpcSubnetSelectorTerms is set"
// +kubebuilder:validation:XValidation:rule="!has(self.vpcSelector) || (has(self.vpcSubnetSelectorTerms) && size(self.vpcSubnetSelectorTerms) > 0)",message="vpcSelector requires at least one entry in vpcSubnetSelectorTerms"
// +kubebuilder:validation:XValidation:rule="!has(self.kubelet) || !has(self.kubelet.cpuManagerPolicy) || self.kubelet.cpuManagerPolicy != 'static' || ((has(self.kubelet.kubeReserved) && has(self.kubelet.kubeReserved.cpu) && self.kubelet.kubeReserved.cpu != '0') || (has(self.kubelet.systemReserved) && has(self.kubelet.systemReserved.cpu) && self.kubelet.systemReserved.cpu != '0'))",message="cpuManagerPolicy 'static' requires a non-zero cpu reservation in kube-reserved or system-reserved"
type ExoscaleNodeClassSpec struct {
	// TemplateID is the instance template to use. Exactly one of templateID
	// and imageTemplateSelector must be set.
	// +optional
	// +kubebuilder:validation:Pattern="^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
	TemplateID string `json:"templateID,omitempty"`

	// ImageTemplateSelector resolves the template from an SKS version and variant.
	// +optional
	ImageTemplateSelector *ImageTemplateSelector `json:"imageTemplateSelector,omitempty"`

	// DiskSize is the root disk size in GB.
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=8000
	// +kubebuilder:default=50
	// +optional
	DiskSize int64 `json:"diskSize,omitempty"`

	// SecurityGroups lists security group IDs to attach.
	// Deprecated: use securityGroupSelectorTerms.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// SecurityGroupSelectorTerms selects the security groups to attach.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	SecurityGroupSelectorTerms []SelectorTerms `json:"securityGroupSelectorTerms,omitempty"`

	// AntiAffinityGroups lists anti-affinity group IDs to join.
	// Deprecated: use antiAffinityGroupSelectorTerms.
	// +optional
	AntiAffinityGroups []string `json:"antiAffinityGroups,omitempty"`

	// AntiAffinityGroupSelectorTerms selects the anti-affinity groups to join.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	AntiAffinityGroupSelectorTerms []SelectorTerms `json:"antiAffinityGroupSelectorTerms,omitempty"`

	// PrivateNetworks lists private network IDs to attach.
	// Deprecated: private networks are deprecated; use vpcSubnetSelectorTerms.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	PrivateNetworks []string `json:"privateNetworks,omitempty"`

	// PrivateNetworkSelectorTerms selects the private networks to attach.
	// Deprecated: private networks are deprecated; use vpcSubnetSelectorTerms.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	PrivateNetworkSelectorTerms []SelectorTerms `json:"privateNetworkSelectorTerms,omitempty"`

	// VpcSelector selects the VPC that holds the vpcSubnetSelectorTerms subnets;
	// an instance can only join subnets of one VPC. Required with vpcSubnetSelectorTerms.
	// +optional
	VpcSelector *SelectorTerms `json:"vpcSelector,omitempty"`

	// VpcSubnetSelectorTerms selects the subnets of vpcSelector's VPC to attach
	// at instance creation.
	// +optional
	// +kubebuilder:validation:MaxItems=7
	VpcSubnetSelectorTerms []SelectorTerms `json:"vpcSubnetSelectorTerms,omitempty"`

	// ElasticIPSelectorTerms selects the elastic IPs to attach.
	// +optional
	// +kubebuilder:validation:MaxItems=10
	ElasticIPSelectorTerms []SelectorTerms `json:"elasticIPSelectorTerms,omitempty"`

	// Kubelet configures the kubelet at node bootstrap.
	// +optional
	Kubelet KubeletConfiguration `json:"kubelet,omitempty"`

	// EnableIPv6 gives instances a public IPv6 address. Changing it replaces existing nodes.
	// +optional
	EnableIPv6 bool `json:"enableIPv6,omitempty"`

	// EnableIPForwarding lets instances route traffic in a VPC network, e.g. as
	// NAT gateways. Changing it replaces existing nodes.
	// +optional
	EnableIPForwarding bool `json:"enableIPForwarding,omitempty"`

	// UserData is TOML merged into the generated node configuration, for settings
	// this API doesn't expose (e.g. static pods). Values Karpenter generates win
	// on conflict. Can't be combined with containerRegistry.
	// +optional
	UserData *string `json:"userData,omitempty"`

	// ContainerRegistry configures containerd mirrors and pull credentials per
	// registry. Secrets they reference must be in kube-system.
	// +optional
	ContainerRegistry *ContainerRegistrySpec `json:"containerRegistry,omitempty"`
}

type ContainerRegistrySpec struct {
	// Mirrors lists, per registry, the endpoints containerd pulls from instead.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	Mirrors []ContainerRegistryMirror `json:"mirrors,omitempty"`
	// Credentials lists, per registry, the credentials containerd pulls with.
	// +optional
	// +kubebuilder:validation:MaxItems=50
	Credentials []ContainerRegistryCredential `json:"credentials,omitempty"`
}

type ContainerRegistryMirror struct {
	// Registry is the registry host the mirror stands in for, e.g. docker.io.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9._-]+$"
	Registry string `json:"registry"`
	// Endpoints of the mirror, tried in order.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=10
	Endpoints []ContainerRegistryMirrorEndpoint `json:"endpoints"`
}

type ContainerRegistryMirrorEndpoint struct {
	// URL of the mirror.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^https?://.+"
	URL string `json:"url"`
	// TLSSecretRef names a kube-system Secret with an optional ca.crt to trust,
	// and an optional tls.crt/tls.key pair for client authentication.
	// +optional
	TLSSecretRef *v1.SecretReference `json:"tlsSecretRef,omitempty"`
	// OverridePath uses the URL path as is instead of appending /v2.
	// +optional
	OverridePath bool `json:"overridePath,omitempty"`
	// SkipVerify disables TLS certificate verification.
	// +optional
	SkipVerify bool `json:"skipVerify,omitempty"`
}

// ContainerRegistryCredential sets exactly one of basic, auth or identityToken.
type ContainerRegistryCredential struct {
	// Registry is the registry host the credentials are for.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern="^[a-zA-Z0-9._-]+$"
	Registry string `json:"registry"`
	// Basic is a username and password.
	// +optional
	Basic *ContainerRegistryBasicAuth `json:"basic,omitempty"`
	// Auth is a base64-encoded "username:password" string.
	// +optional
	Auth *ContainerRegistryAuth `json:"auth,omitempty"`
	// IdentityToken is a registry identity (refresh) token.
	// +optional
	IdentityToken *ContainerRegistryIdentityToken `json:"identityToken,omitempty"`
}

type ContainerRegistryBasicAuth struct {
	// +kubebuilder:validation:Required
	UsernameSecretRef v1.SecretKeySelector `json:"usernameSecretRef"`
	// +kubebuilder:validation:Required
	PasswordSecretRef v1.SecretKeySelector `json:"passwordSecretRef"`
}

type ContainerRegistryAuth struct {
	AuthSecretRef v1.SecretKeySelector `json:"authSecretRef"`
}
type ContainerRegistryIdentityToken struct {
	IdentityTokenSecretRef v1.SecretKeySelector `json:"identityTokenSecretRef"`
}

// KubeletConfiguration holds kubelet settings applied at node bootstrap.
// +kubebuilder:validation:XValidation:rule="!has(self.cpuManagerPolicyOptions) || self.cpuManagerPolicy == 'static'",message="cpuManagerPolicyOptions requires cpuManagerPolicy to be 'static'"
type KubeletConfiguration struct {
	// ClusterDNS lists the cluster DNS server IPs.
	// +kubebuilder:default={"10.96.0.10"}
	// +optional
	ClusterDNS []string `json:"clusterDNS,omitempty"`

	// ImageGCHighThresholdPercent is the disk usage that starts image garbage collection.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=85
	// +optional
	ImageGCHighThresholdPercent *int32 `json:"imageGCHighThresholdPercent,omitempty"`

	// ImageGCLowThresholdPercent is the disk usage that image garbage collection frees down to.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=80
	// +optional
	ImageGCLowThresholdPercent *int32 `json:"imageGCLowThresholdPercent,omitempty"`

	// ImageMinimumGCAge is how long an unused image is kept before it can be collected, e.g. 2m.
	// +kubebuilder:default="2m"
	// +kubebuilder:validation:Pattern="^[0-9]+(s|m|h)$"
	// +optional
	ImageMinimumGCAge string `json:"imageMinimumGCAge,omitempty"`

	// KubeReserved is reserved for Kubernetes components.
	// +optional
	KubeReserved KubeResourceReservation `json:"kubeReserved,omitempty"`

	// SystemReserved is reserved for operating system components.
	// +optional
	SystemReserved SystemResourceReservation `json:"systemReserved,omitempty"`

	// FeatureGates turns kubelet feature gates on or off.
	// +optional
	FeatureGates map[string]bool `json:"featureGates,omitempty"`

	// CPUManagerPolicy is the kubelet CPU manager policy. static needs a non-zero
	// CPU reservation in kubeReserved or systemReserved.
	// +kubebuilder:validation:Enum=none;static
	// +optional
	CPUManagerPolicy string `json:"cpuManagerPolicy,omitempty"`

	// CPUManagerPolicyOptions tunes the static policy; requires cpuManagerPolicy static.
	// +kubebuilder:validation:items:Enum=full-pcpus-only;distribute-cpus-across-numa;prefer-align-cpus-by-uncorecache;strict-cpu-reservation;align-by-socket;distribute-cpus-across-cores
	// +optional
	// +listType=atomic
	CPUManagerPolicyOptions []string `json:"cpuManagerPolicyOptions,omitempty"`

	// CPUManagerReconcilePeriod is how often the kubelet reconciles CPU assignments.
	// +kubebuilder:default="10s"
	// +kubebuilder:validation:Pattern="^[0-9]+(ns|us|ms|s|m|h)$"
	// +optional
	CPUManagerReconcilePeriod string `json:"cpuManagerReconcilePeriod,omitempty"`

	// MaxPods is the kubelet's maximum number of pods. Defaults to the kubelet's own default.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	MaxPods *int32 `json:"maxPods,omitempty"`
}

type KubeResourceReservation struct {
	// CPU reserved for Kubernetes components.
	// +kubebuilder:default="200m"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(m|[.])?(\\d+)?|\\d+m)$"
	CPU string `json:"cpu,omitempty"`

	// Memory reserved for Kubernetes components.
	// +kubebuilder:default="300Mi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	Memory string `json:"memory,omitempty"`

	// EphemeralStorage reserved for Kubernetes components.
	// +kubebuilder:default="1Gi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
}

type SystemResourceReservation struct {
	// CPU reserved for operating system components.
	// +kubebuilder:default="100m"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(m|[.])?(\\d+)?|\\d+m)$"
	CPU string `json:"cpu,omitempty"`

	// Memory reserved for operating system components.
	// +kubebuilder:default="100Mi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	Memory string `json:"memory,omitempty"`

	// EphemeralStorage reserved for operating system components.
	// +kubebuilder:default="3Gi"
	// +optional
	// +kubebuilder:validation:Pattern="^(\\d+(Ki|Mi|Gi|Ti|Pi|Ei|k|M|G|T|P|E)?)$"
	EphemeralStorage string `json:"ephemeralStorage,omitempty"`
}

func (in *ExoscaleNodeClass) StatusConditions(_ ...status.ForOption) status.ConditionSet {
	return status.NewReadyConditions().For(in)
}

func (in *ExoscaleNodeClass) GetConditions() []status.Condition {
	return in.Status.Conditions
}

func (in *ExoscaleNodeClass) SetConditions(conditions []status.Condition) {
	in.Status.Conditions = conditions
}

// ExoscaleNodeClassStatus is the resolved state of the NodeClass.
type ExoscaleNodeClassStatus struct {
	// Conditions reports readiness and why resolution failed.
	Conditions []status.Condition `json:"conditions,omitempty"`

	// SecurityGroups are the resolved security group IDs.
	// +optional
	SecurityGroups []string `json:"securityGroups,omitempty"`

	// AntiAffinityGroups are the resolved anti-affinity group IDs.
	// +optional
	AntiAffinityGroups []string `json:"antiAffinityGroups,omitempty"`

	// PrivateNetworks are the resolved private network IDs.
	// +optional
	PrivateNetworks []string `json:"privateNetworks,omitempty"`

	// VpcID is the resolved VPC ID, set when VPC subnets are selected.
	// +optional
	// +kubebuilder:validation:Pattern="^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
	VpcID string `json:"vpcID,omitempty"`

	// VpcSubnets are the resolved VPC subnet IDs.
	// +optional
	VpcSubnets []string `json:"vpcSubnets,omitempty"`

	// ElasticIPs are the resolved elastic IP IDs.
	// +optional
	// +kubebuilder:validation:items:Pattern="^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
	ElasticIPs []string `json:"elasticIPs,omitempty"`

	// ConfigurationHash is a SHA-256 of the settings baked into node bootstrap
	// (container registry and its Secrets, CPU manager, maxPods). NodeClaims
	// whose karpenter.exoscale.com/configuration-hash annotation differs are drifted.
	// +optional
	ConfigurationHash string `json:"configurationHash,omitempty"`

	// ImageID is the resolved template ID. NodeClaims launched from another
	// template are drifted.
	// +optional
	ImageID string `json:"imageID,omitempty"`
}

// ExoscaleNodeClassList is a list of ExoscaleNodeClass.
// +kubebuilder:object:root=true
type ExoscaleNodeClassList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ExoscaleNodeClass `json:"items"`
}

// DefaultCPUManagerReconcilePeriod is the kubelet default for
// --cpu-manager-reconcile-period. It is mirrored by the
// +kubebuilder:default marker on KubeletConfiguration.CPUManagerReconcilePeriod
// and must be kept in sync with it. The reconciler and the bootstrap TOML
// emitter both consult this constant to decide whether a non-empty value is
// user-supplied or just the apiserver-injected default; matching the kubelet
// default keeps the resulting user-data and the drift hash sparse.
const DefaultCPUManagerReconcilePeriod = "10s"
