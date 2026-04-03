# ExoscaleNodeClass

`ExoscaleNodeClass` is a cluster-scoped custom resource that defines the infrastructure configuration for nodes provisioned by Karpenter on Exoscale.

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
```

## Table of Contents

- [Spec Fields](#spec-fields)
  - [`spec.templateID`](#spectemplateID)
  - [`spec.imageTemplateSelector`](#specimageTemplateSelector)
  - [`spec.diskSize`](#specdiskSize)
  - [`spec.securityGroupSelectorTerms`](#specsecurityGroupSelectorTerms)
  - [`spec.antiAffinityGroupSelectorTerms`](#specantiAffinityGroupSelectorTerms)
  - [`spec.privateNetworkSelectorTerms`](#specprivateNetworkSelectorTerms)
  - [`spec.kubelet`](#speckubelet)
  - [`spec.userData`](#specuserData)
- [Status Fields](#status-fields)

---

## Spec Fields

### `spec.templateID`

**Type**: String (UUID)
**Required**: Yes (mutually exclusive with `spec.imageTemplateSelector`)

The UUID of the VM template to use for provisioned instances. Exactly one of `templateID` or `imageTemplateSelector` must be specified.

```yaml
spec:
  templateID: "20000000-0000-0000-0000-000000000001"
```

This option allows to use custom templates belonging to user's organization.

The OS template must include an initialization agent that consumes user-data in the TOML format described in [user-data.md](user-data.md). The user-data may be gzip-compressed and base64-encoded.

Karpenter generates user-data that relies on the following subset of `settings.kubernetes.*` parameters. The agent **must** support them for nodes to join the cluster correctly:

| Setting                                               | Required | Description                                                                      |
|-------------------------------------------------------|----------|----------------------------------------------------------------------------------|
| `settings.kubernetes.api-server`                      | **Yes**  | URL of the Kubernetes API server.                                                |
| `settings.kubernetes.bootstrap-token`                 | **Yes**  | TLS bootstrap token for kubelet registration.                                    |
| `settings.kubernetes.cloud-provider`                  | **Yes**  | Always set to `"external"` (Exoscale CCM).                                       |
| `settings.kubernetes.cluster-certificate`             | **Yes**  | Base64-encoded cluster CA certificate.                                           |
| `settings.kubernetes.cluster-dns-ip`                  |   No     | Cluster DNS IP(s). Set when `spec.kubelet.clusterDNS` is configured.             |
| `settings.kubernetes.cluster-domain`                  |   No     | Cluster DNS domain. Set when the controller `--cluster-domain` flag is provided. |
| `settings.kubernetes.node-labels`                     |   No     | Labels from the NodePool and NodeClaim.                                          |
| `settings.kubernetes.node-taints`                     |   No     | Taints from the NodeClaim.                                                       |
| `settings.kubernetes.image-gc-high-threshold-percent` |   No     | Set when `spec.kubelet.imageGCHighThresholdPercent` is configured.               |
| `settings.kubernetes.image-gc-low-threshold-percent`  |   No     | Set when `spec.kubelet.imageGCLowThresholdPercent` is configured.                |
| `settings.kubernetes.image-minimum-gc-age`            |   No     | Set when `spec.kubelet.imageMinimumGCAge` is configured.                         |
| `settings.kubernetes.kube-reserved`                   |   No     | Set when `spec.kubelet.kubeReserved` has any value.                              |
| `settings.kubernetes.system-reserved`                 |   No     | Set when `spec.kubelet.systemReserved` has any value.                            |
| `settings.kubernetes.feature-gates`                   |   No     | Set when `spec.kubelet.featureGates` is configured.                              |

The four **required** settings are always present in the generated user-data. Optional settings are included only when the corresponding `spec.kubelet` field is set; the agent should apply sensible defaults when they are absent.

> [!IMPORTANT]
> Custom OS images that do not implement at least the four required settings will produce nodes that fail to join the cluster.

---

### `spec.imageTemplateSelector`

**Type**: Object
**Required**: Yes (mutually exclusive with `spec.templateID`)

Selects a VM template dynamically by version and variant instead of a hardcoded UUID. Exactly one of `templateID` or `imageTemplateSelector` must be specified.

This option resolves template IDs actually in use by SKS.

| Field     | Type   | Default      | Description                                          |
|-----------|--------|--------------|------------------------------------------------------|
| `version` | String | *(optional)* | Kubernetes version in `X.Y.Z` semver format.         |
| `variant` | String | `standard`   | Image variant. Allowed values: `standard`, `nvidia`. |

```yaml
spec:
  imageTemplateSelector:
    version: "1.33.0"
    variant: nvidia
```

---

### `spec.diskSize`

**Type**: Integer
**Default**: `50`
**Minimum**: `10`
**Maximum**: `8000` (depends on the instance offering)

The size of the root disk in GB.

```yaml
spec:
  diskSize: 100
```

---

### `spec.securityGroupSelectorTerms`

**Type**: List of selector terms
**Max items**: 50

Selects security groups to attach to instances by ID or name. Each term must specify exactly one of `id` or `name`.

```yaml
spec:
  securityGroupSelectorTerms:
    - name: "my-security-group"
    - id: "30000000-0000-0000-0000-000000000001"
```

> [!NOTE]
> The deprecated `spec.securityGroups` field (list of UUIDs) is still accepted but will be removed in a future release.

---

### `spec.antiAffinityGroupSelectorTerms`

**Type**: List of selector terms
**Max items**: 50

Selects anti-affinity groups by ID or name. Anti-affinity groups ensure instances are placed on different hypervisors for high availability.

```yaml
spec:
  antiAffinityGroupSelectorTerms:
    - name: "my-anti-affinity-group"
```

> [!NOTE]
> The deprecated `spec.antiAffinityGroups` field (list of UUIDs) is still accepted but will be removed in a future release.

---

### `spec.privateNetworkSelectorTerms`

**Type**: List of selector terms
**Max items**: 10

Selects private networks to attach to instances by ID or name.

```yaml
spec:
  privateNetworkSelectorTerms:
    - name: "my-private-network"
```

> [!NOTE]
> The deprecated `spec.privateNetworks` field (list of UUIDs) is still accepted but will be removed in a future release.

---

### `spec.kubelet`

**Type**: Object

Configuration for the kubelet running on provisioned nodes.

#### `spec.kubelet.clusterDNS`

**Type**: List of Strings
**Default**: `["10.96.0.10"]`

IP addresses of the cluster DNS server.

#### `spec.kubelet.imageGCHighThresholdPercent`

**Type**: Integer (0-100)
**Default**: `85`

Disk usage percentage at which image garbage collection is triggered.

#### `spec.kubelet.imageGCLowThresholdPercent`

**Type**: Integer (0-100)
**Default**: `80`

Disk usage percentage below which image garbage collection stops.

> [!IMPORTANT]
> `imageGCLowThresholdPercent` must be strictly less than `imageGCHighThresholdPercent`.

#### `spec.kubelet.imageMinimumGCAge`

**Type**: String (duration)
**Default**: `"2m"`
**Pattern**: `^[0-9]+(s|m|h)$`

Minimum age for an unused image before it can be garbage collected.

#### `spec.kubelet.kubeReserved`

Resources reserved for Kubernetes system components.

| Field              | Type   | Default  | Description                           |
|--------------------|--------|----------|---------------------------------------|
| `cpu`              | String | `"200m"` | CPU reservation (e.g. `"200m"`)       |
| `memory`           | String | `"300Mi"`| Memory reservation (e.g. `"300Mi"`)   |
| `ephemeralStorage` | String | `"1Gi"`  | Ephemeral storage reservation         |

#### `spec.kubelet.systemReserved`

Resources reserved for OS system components.

| Field              | Type   | Default  | Description                           |
|--------------------|--------|----------|---------------------------------------|
| `cpu`              | String | `"100m"` | CPU reservation (e.g. `"100m"`)       |
| `memory`           | String | `"100Mi"`| Memory reservation (e.g. `"100Mi"`)   |
| `ephemeralStorage` | String | `"3Gi"`  | Ephemeral storage reservation         |

#### `spec.kubelet.featureGates`

**Type**: Map of String to Boolean

Kubernetes [feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) to enable or disable on the kubelet.

```yaml
spec:
  kubelet:
    clusterDNS: ["10.96.0.10"]
    imageGCHighThresholdPercent: 85
    imageGCLowThresholdPercent: 80
    imageMinimumGCAge: "2m"
    kubeReserved:
      cpu: "200m"
      memory: "300Mi"
      ephemeralStorage: "1Gi"
    systemReserved:
      cpu: "100m"
      memory: "100Mi"
      ephemeralStorage: "3Gi"
    featureGates:
      ImageVolume: true
```

---

### `spec.userData`

**Type**: String (raw TOML)

Optional raw TOML content that is deep-merged with the Karpenter-generated user-data configuration.
This allows specifying additional settings that are not directly exposed by the ExoscaleNodeClass API, such as NVIDIA device plugin configuration.

#### Merge Behavior

The user-provided TOML is deep-merged with the Karpenter-generated configuration:
- **Karpenter-managed fields always take precedence**: `api-server`, `bootstrap-token`, `cloud-provider`, `cluster-certificate`, and other fields managed through `spec.kubelet` cannot be overridden.
- **User-provided sections are preserved**: settings that Karpenter does not manage (e.g. `settings.kubelet-device-plugins`) are included as-is in the final user-data.
- **Maps are merged recursively**: for example, user-provided `node-labels` are merged with Karpenter-managed labels, but Karpenter labels win on conflict.

See [user-data.md](user-data.md) for the full list of settings available.

#### Example: NVIDIA GPU Time-Slicing

```yaml
spec:
  imageTemplateSelector:
    version: "1.33.0"
    variant: nvidia
  diskSize: 100
  userData: |
    [settings.kubelet-device-plugins.nvidia]
    device-sharing-strategy = "time-slicing"

    [settings.kubelet-device-plugins.nvidia.time-slicing]
    replicas = 4
    rename-by-default = false
    fail-requests-greater-than-one = true
```

---

## Status Fields

The `status` subresource is managed by the controller and reflects the resolved state.

| Field                | Type             | Description                                                |
|----------------------|------------------|------------------------------------------------------------|
| `conditions`         | List (Condition) | Health and readiness signals (includes `Ready`).           |
| `securityGroups`     | List of Strings  | Resolved security group IDs after applying selectors.      |
| `antiAffinityGroups` | List of Strings  | Resolved anti-affinity group IDs after applying selectors. |
| `privateNetworks`    | List of Strings  | Resolved private network IDs after applying selectors.     |
