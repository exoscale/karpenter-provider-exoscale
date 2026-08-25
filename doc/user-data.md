# `sks-node-agent` Settings Reference

## Table of Contents

- [Overview](#overview)
- [Configuration Parameters](#configuration-parameters)
  - [Kubernetes Settings (`settings.kubernetes.*`)](#kubernetes-settings-settingskubernetes)
    - [`settings.kubernetes.api-server`](#settingskubernetesapi-server)
    - [`settings.kubernetes.bootstrap-token`](#settingskubernetesbootstrap-token)
    - [`settings.kubernetes.cloud-provider`](#settingskubernetescloud-provider)
    - [`settings.kubernetes.cluster-certificate`](#settingskubernetescluster-certificate)
    - [`settings.kubernetes.cluster-dns-ip`](#settingskubernetescluster-dns-ip)
    - [`settings.kubernetes.cluster-domain`](#settingskubernetescluster-domain)
    - [`settings.kubernetes.cpu-manager-policy`](#settingskubernetescpu-manager-policy)
    - [`settings.kubernetes.cpu-manager-policy-options`](#settingskubernetescpu-manager-policy-options)
    - [`settings.kubernetes.cpu-manager-reconcile-period`](#settingskubernetescpu-manager-reconcile-period)
    - [`settings.kubernetes.feature-gates`](#settingskubernetesfeature-gates)
    - [`settings.kubernetes.image-gc-high-threshold-percent`](#settingskubernetesimage-gc-high-threshold-percent)
    - [`settings.kubernetes.image-gc-low-threshold-percent`](#settingskubernetesimage-gc-low-threshold-percent)
    - [`settings.kubernetes.image-minimum-gc-age`](#settingskubernetesimage-minimum-gc-age)
    - [`settings.kubernetes.kube-reserved`](#settingskuberneteskube-reserved)
    - [`settings.kubernetes.max-pods`](#settingskubernetesmax-pods)
    - [`settings.kubernetes.node-labels`](#settingskubernetesnode-labels)
    - [`settings.kubernetes.node-taints`](#settingskubernetesnode-taints)
    - [`settings.kubernetes.static-pods.<identifier>`](#settingskubernetesstatic-podsidentifier)
    - [`settings.kubernetes.standalone-mode`](#settingskubernetesstandalone-mode)
    - [`settings.kubernetes.system-reserved`](#settingskubernetessystem-reserved)
  - [Container Registry Settings (`settings.container-registry.*`)](#container-registry-settings-settingscontainer-registry)
    - [`settings.container-registry.mirrors`](#settingscontainer-registrymirrors)
    - [`settings.container-registry.tls`](#settingscontainer-registrytls)
    - [`settings.container-registry.credentials`](#settingscontainer-registrycredentials)
  - [Kubelet Device Plugins Settings (`settings.kubelet-device-plugins.*`)](#kubelet-device-plugins-settings-settingskubelet-device-plugins)
    - [`settings.kubelet-device-plugins.nvidia` (EXPERIMENTAL)](#settingskubelet-device-pluginsnvidia-experimental)
    - [`settings.kubelet-device-plugins.nvidia.time-slicing` (EXPERIMENTAL)](#settingskubelet-device-pluginsnvidiatime-slicing-experimental)
    - [`settings.kubelet-device-plugins.nvidia.mig` (EXPERIMENTAL)](#settingskubelet-device-pluginsnvidiamig-experimental)
- [Additional Resources](#additional-resources)

## Overview

When an instance starts, it retrieves `user-data` from the [metadata server](https://community.exoscale.com/documentation/compute/cloud-init/#querying-the-user-data-and-meta-data-from-the-instance).
This `user-data` is used during the initialization process to configure the instance according to specified parameters.

The `user-data` can be supplied in different formats:
- **TOML File**: Optionally gzip-compressed and base64-encoded.
- **Base64-Encoded String**: Without compression.
- **Plain Text**: Unencoded and uncompressed.

We provide support for gzipped and base64-encoded `user-data` because of the way out Terraform provider encodes this content by default.

An initialization program called `sks-node-agent`, processes the `user-data`.
It reads the provided configurations and applies them to the system, setting up features such as Kubernetes node parameters and storage configurations.

# Configuration Parameters

The `user-data` file allows you to specify various settings under two main categories: **Exoscale-specific settings** (not documented here since they are not actively maintained) and **Kubernetes-related settings**.

## Kubernetes Settings (`settings.kubernetes.*`)

These settings are related to the Kubernetes configuration of the node. They allow you to customize the Kubelet behavior, networking, and other Kubernetes-specific features.

> [!NOTE]
> Most of these options are inspired by the [Bottlerocket](https://bottlerocket.dev/en/os/1.20.x/api/settings/kubernetes/) open-source project.

### `settings.kubernetes.api-server`

**Type**: String

The URL of the Kubernetes API server that this node should connect to.

- **Example**:

  ```toml
  [settings.kubernetes]
  api-server = "https://api.your-k8s-cluster.example.com:6443"
  ```

### `settings.kubernetes.bootstrap-token`

**Type**: String

The token used for [TLS bootstrapping](https://kubernetes.io/docs/reference/access-authn-authz/kubelet-tls-bootstrapping/).

- **Example**:

  ```toml
  [settings.kubernetes]
  bootstrap-token = "abcdef.0123456789abcdef"
  ```

### `settings.kubernetes.cloud-provider`

**Type**: String

Specifies the cloud provider for the Kubernetes cluster.
This should typically be set to `external` because the cloud provider integration is handled externally via the Exoscale Cloud Controller Manager.
Any other value disable cloud provider integration.

- **Example**:

  ```toml
  [settings.kubernetes]
  cloud-provider = "external"
  ```

### `settings.kubernetes.cluster-certificate`

**Type**: String (Base64-encoded)

The base64-encoded CA certificate for the Kubernetes cluster. This certificate is used by the kubelet to verify the API server's certificate.

- **Example**:

  ```toml
  [settings.kubernetes]
  cluster-certificate = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0t..."
  ```

### `settings.kubernetes.cluster-dns-ip`

**Type**: String or Array of String

The IP of the DNS service running in the cluster. This value can be set as a string containing a single IP address, or as a list containing multiple IP addresses.

- **Default**: `10.96.0.10`

- **Example**:

  ```toml
  [settings.kubernetes]
  cluster-dns-ip = "10.0.0.1"
  ```

  ```toml
  [settings.kubernetes]
  cluster-dns-ip = ["10.0.0.1", "fd00:ea00::10"]
  ```

### `settings.kubernetes.cluster-domain`

**Type**: String

The DNS domain for the Kubernetes cluster.

- **Default**: `cluster.local`

- **Example**:

  ```toml
  [settings.kubernetes]
  cluster-domain = "cluster.local"
  ```

### `settings.kubernetes.cpu-manager-policy`

**Type**: String

Selects the [CPU management policy](https://kubernetes.io/docs/tasks/administer-cluster/cpu-management-policies/) for the kubelet.
Allowed values are `"none"` (default) and `"static"`.

`"static"` requires a non-zero `cpu` reservation in `kube-reserved` or `system-reserved`.

- **Example**:

  ```toml
  [settings.kubernetes]
  cpu-manager-policy = "static"
  ```

### `settings.kubernetes.cpu-manager-policy-options`

**Type**: Array of String

Fine-tunes the static CPU manager policy. Ignored unless `cpu-manager-policy` is set to `"static"`.

Each entry must be one of: `full-pcpus-only`, `distribute-cpus-across-numa`, `prefer-align-cpus-by-uncorecache`, `strict-cpu-reservation`, `align-by-socket`, `distribute-cpus-across-cores`.

- **Example**:

  ```toml
  [settings.kubernetes]
  cpu-manager-policy = "static"
  cpu-manager-policy-options = ["full-pcpus-only", "align-by-socket"]
  ```

### `settings.kubernetes.cpu-manager-reconcile-period`

**Type**: String (Duration)

How often the kubelet reconciles CPU assignments under the static policy. The kubelet built-in default (`10s`) is used when unset.

- **Example**:

  ```toml
  [settings.kubernetes]
  cpu-manager-reconcile-period = "5s"
  ```

### `settings.kubernetes.feature-gates`

**Type**: Map of String/Boolean pairs

Defines [feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/) as key-value pairs to configure Kubelet component.

> [!IMPORTANT]
> The Feature Gates list **MUST** be validated prior to defining
> `settings.kubernetes.feature-gates` since no further validation is performed
> by the image/sks-node-agent itself.

- **Example**:

  ```toml
  [settings.kubernetes.feature-gates]
  "ImageVolume" = true
  "MemoryQoS" = true
  ```

### `settings.kubernetes.image-gc-high-threshold-percent`

**Type**: Integer (0-100)

Specifies the disk usage percentage at which the kubelet will start garbage collecting unused container images.

- **Example**:

  ```toml
  [settings.kubernetes]
  image-gc-high-threshold-percent = 85
  ```

### `settings.kubernetes.image-gc-low-threshold-percent`

**Type**: Integer (0-100)

Specifies the disk usage percentage below which the kubelet will stop garbage collecting images.

- **Example**:

  ```toml
  [settings.kubernetes]
  image-gc-low-threshold-percent = 80
  ```

### `settings.kubernetes.image-minimum-gc-age`

**Type**: String (Duration)

Defines the minimum age that an unused image must have before it is eligible for garbage collection.

- **Example**:

  ```toml
  [settings.kubernetes]
  image-minimum-gc-age = "5m"
  ```

### `settings.kubernetes.kube-reserved`

Resources reserved for node components.

- **Parameters**:
  - `cpu` (string): defaults to `200m`.
  - `memory` (string): defaults to `300Mi`.
  - `ephemeral-storage` (string): defaults to `1Gi`.

- **Example**:

  ```toml
  [settings.kubernetes.kube-reserved]
  cpu = "200m"
  memory = "300Mi"
  ephemeral-storage = "1Gi"
  ```

### `settings.kubernetes.max-pods`

**Type**: Integer (1-255)

Sets the maximum number of pods that can run on this node.

The kubelet built-in default is used when the setting is absent. Must be between `1` and `255` inclusive.

- **Example**:

  ```toml
  [settings.kubernetes]
  max-pods = 110
  ```

### `settings.kubernetes.node-labels`

**Type**: Map of Strings

Defines [node labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) as key-value pairs to be assigned to the node upon registration.

- **Example**:

  ```toml
  [settings.kubernetes.node-labels]
  "environment" = "production"
  "region" = "ch-gva-2"
  "a40-gpu" = "true"
  ```

### `settings.kubernetes.node-taints`

**Type**: Map of Lists of Strings

Specifies [node taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) to control pod scheduling on the node.
Each key is a taint key, and the value is a list of taint values combined with effects.

- **Example**:

  ```toml
  [settings.kubernetes.node-taints]
  "dedicated" = ["experimental:PreferNoSchedule", "experimental:NoExecute"]
  "maintenance" = ["true:NoSchedule"]
  ```

### `settings.kubernetes.static-pods.<identifier>`

Specify a static pod with a unique `<identifier>`.
The `<identifier>` must satisfy this regex: `^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`.

- **Parameters**:
  - `enabled` (boolean): Enable or disable the static pod configuration.
  - `manifest` (string): The base64-encoded YAML manifest of the static pod.

- **Example**:

  ```toml
  [settings.kubernetes.static-pods.my-static-pod]
  enabled = true
  manifest = "YXBpVmVyc2lvbjogdjEKa2luZDogUG9kCm1ldGFkYXRhOgogIG5hbWU6IG15LXN0YXRpYy1wb2QKCg==..."
  ```

### `settings.kubernetes.standalone-mode`

It `true`, kubelet runs in standalone mode without connecting to an API server.

**Type**: Boolean

**Default**: `false`

### `settings.kubernetes.system-reserved`

Resources reserved for system components.

- **Parameters**:
  - `cpu` (string): defaults to `100m`.
  - `memory` (string): defaults to `100Mi`.
  - `ephemeral-storage` (string): defaults to `3Gi`.

- **Example**:

  ```toml
  [settings.kubernetes.kube-reserved]
  cpu = "100m"
  memory = "100Mi"
  ephemeral-storage = "3Gi"
  ```

## Container Registry Settings (`settings.container-registry.*`)

These settings configure [containerd registry](https://github.com/containerd/containerd/blob/main/docs/hosts.md) mirrors, TLS material and credentials so that kubelets can pull images through private registries or accelerated public mirrors.

The agent hot-reloads `hosts.toml` changes (mirror re-resolution requires no node restart), but `config.toml` changes (credentials) trigger a `containerd` restart.

> [!IMPORTANT]
> Credentials and TLS material are sourced from Kubernetes Secrets referenced from `spec.containerRegistry` on the `ExoscaleNodeClass`. They are **never** stored inline in the generated user-data; the Secrets must live in the `kube-system` namespace. See `spec.containerRegistry` in `nodeclass.md` for the full schema.

### `settings.container-registry.mirrors`

**Type**: Array of Tables

Each table defines a per-registry mirror. containerd rewrites image pulls whose hostname matches the configured `registry` and forwards them to one or more endpoints in order.

- **Parameters** (per table):
  - `registry` (string, required): Hostname rewritten by containerd (e.g. `"docker.io"`).
  - `endpoint` (array of string, required): Ordered list of mirror endpoints (`"https://..."`).

- **Example**:

  ```toml
  [[settings.container-registry.mirrors]]
  registry = "docker.io"
  endpoint = ["https://mirror.example.com"]

  [[settings.container-registry.mirrors]]
  registry = "gcr.io"
  endpoint = ["https://gcr-mirror.example.com", "https://gcr-mirror-2.example.com"]
  ```

### `settings.container-registry.tls`

**Type**: Map of Tables

Per-mirror TLS overrides. The key is the mirror endpoint URL (`"https://..."`); the value holds the optional CA, client certificate and client key, plus connection-tuning flags.

- **Parameters** (per table):
  - `ca` (string): PEM-encoded CA bundle.
  - `cert` (string): PEM-encoded client certificate.
  - `key` (string): PEM-encoded client key.
  - `override_path` (boolean): Override the URL path component. Default: `false`.
  - `skip_verify` (boolean): Skip TLS verification. Default: `false`.

- **Example**:

  ```toml
  [settings.container-registry.tls."https://mirror.example.com"]
  ca = """-----BEGIN CERTIFICATE-----
  ...
  -----END CERTIFICATE-----"""
  skip_verify = false
  ```

### `settings.container-registry.credentials`

**Type**: Array of Tables

Per-registry pull credentials. Each entry must declare exactly one of `username`/`password`, `auth`, or `identitytoken`; the others must be omitted.

- **Parameters** (per table):
  - `registry` (string, required): Hostname the credential applies to.
  - `username` (string): Username (paired with `password`).
  - `password` (string): Password (paired with `username`).
  - `auth` (string): Pre-encoded `user:pass` string (typically base64 of `username:password`).
  - `identitytoken` (string): OAuth2-style identity token.

- **Example**:

  ```toml
  [[settings.container-registry.credentials]]
  registry = "reg.example.com"
  username = "robot"
  password = "s3cr3t"

  [[settings.container-registry.credentials]]
  registry = "gcr.io"
  auth = "X2FwcGE6c2VjcmV0Cg=="

  [[settings.container-registry.credentials]]
  registry = "ghcr.io"
  identitytoken = "ghp_xxxxxxxxxxxxxxxxxxxx"
  ```

## Kubelet Device Plugins Settings (`settings.kubelet-device-plugins.*`)

These settings configure device plugins for Kubernetes. Currently only nvidia device plugin is supported.

### `settings.kubelet-device-plugins.nvidia` (EXPERIMENTAL)

Configures the NVIDIA Kubernetes device plugin behavior.

- **Parameters**:
  - `device-sharing-strategy` (string): GPU sharing strategy. Options: `"none"` (default), `"time-slicing"`, `"mps"`
  - `device-partitioning-strategy` (string): MIG partitioning strategy. Options: `"none"` (default), `"mig"`
  - `device-list-strategy` (string): How device list is exposed to containers. Options: `"envvar"` (default), `"volume-mounts"`
  - `device-id-strategy` (string): Device ID exposure strategy. Options: `"index"` (default), `"uuid"`
  - `pass-device-specs` (boolean): Pass device specifications to the container runtime. Default: `true`

- **Example**:

  ```toml
  [settings.kubelet-device-plugins.nvidia]
  device-sharing-strategy = "none"
  device-partitioning-strategy = "none"
  device-list-strategy = "envvar"
  device-id-strategy = "index"
  pass-device-specs = true
  ```

### `settings.kubelet-device-plugins.nvidia.time-slicing` (EXPERIMENTAL)

Configures time-slicing parameters when `device-sharing-strategy` is set to `"time-slicing"`. Time-slicing allows multiple containers to share a single GPU by time-multiplexing GPU access.

- **Parameters**:
  - `replicas` (integer): Number of virtual GPUs to create per physical GPU. Default: `1`
  - `rename-by-default` (boolean): Rename shared GPU resources. Default: `false`
  - `fail-requests-greater-than-one` (boolean): Fail pod requests asking for more than one GPU. Default: `false`

- **Example**:

  ```toml
  [settings.kubelet-device-plugins.nvidia]
  device-sharing-strategy = "time-slicing"
  
  [settings.kubelet-device-plugins.nvidia.time-slicing]
  replicas = 10
  rename-by-default = false
  fail-requests-greater-than-one = false
  ```

  ```toml
  # Enable NVIDIA GPU time-slicing with 4 virtual GPUs per physical GPU
  [settings.kubelet-device-plugins.nvidia]
  device-sharing-strategy = "time-slicing"
  device-list-strategy = "envvar"
  pass-device-specs = false

  [settings.kubelet-device-plugins.nvidia.time-slicing]
  replicas = 4
  rename-by-default = false
  fail-requests-greater-than-one = true  # Prevent pods from requesting multiple GPUs
  ```

### `settings.kubelet-device-plugins.nvidia.mig` (EXPERIMENTAL)

Configures Multi-Instance GPU (MIG) profiles when `device-partitioning-strategy` is set to `"mig"`. MIG allows partitioning of compatible GPUs (A30, RTX pro 6000) into multiple isolated instances.

- **Parameters**:
  - `profile` (map): GPU model to MIG profile mapping
    - Key format: `{gpu-model}.{memory}gb` (e.g., `"a30.24g"`, `"rtxpro6000.96g"`)
    - Value: MIG profile name (e.g., `"2g.12gb"`) or number of partitions (e.g., `"4"`)

- **Example**:

  ```toml
  [settings.kubelet-device-plugins.nvidia]
  device-partitioning-strategy = "mig"

  [settings.kubelet-device-plugins.nvidia.mig.profile]
  "a30.24g" = "2g.12gb"  # Partition A30 24GB into 2g.12gb instances
  "rtxpro6000.96g" = "4" # Partition RTX pro 6000 96GB into 4 equal instances
  ```

> [!NOTE]
> MIG (Multi-Instance GPU) and MPS (Multi-Process Service) are not supported as they require GPUs not available on Exoscale (A100/H100 for MIG).

> [!IMPORTANT]
> The NVIDIA device plugin service requires the kubelet to be running. It will automatically start when kubelet starts and restart if kubelet restarts.

# Additional Resources

- **Kubernetes Official Documentation**: [Kubelet TLS Bootstrapping](https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet-tls-bootstrapping/)
- **Exoscale Cloud Controller Manager**: [GitHub Repository](https://github.com/exoscale/exoscale-cloud-controller-manager)
- **Bottlerocket Settings**: [Bottlerocket OS Settings](https://bottlerocket.dev/docs/settings)
- **NVIDIA Device Plugin for Kubernetes**: [GitHub Repository](https://github.com/NVIDIA/k8s-device-plugin)
- **NVIDIA GPU Sharing Strategies**: [Time-Slicing and MPS Documentation](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/gpu-sharing.html)
- **Bottlerocket NVIDIA Configuration**: [Kubelet Device Plugins Settings](https://bottlerocket.dev/en/os/1.34.x/api/settings/kubelet-device-plugins/)
