# `sks-node-agent` Metadata Settings Reference

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
    - [`settings.kubernetes.image-gc-high-threshold-percent`](#settingskubernetesimage-gc-high-threshold-percent)
    - [`settings.kubernetes.image-gc-low-threshold-percent`](#settingskubernetesimage-gc-low-threshold-percent)
    - [`settings.kubernetes.image-minimum-gc-age`](#settingskubernetesimage-minimum-gc-age)
    - [`settings.kubernetes.kube-reserved`](#settingskuberneteskube-reserved)
    - [`settings.kubernetes.node-labels`](#settingskubernetesnode-labels)
    - [`settings.kubernetes.node-taints`](#settingskubernetesnode-taints)
    - [`settings.kubernetes.static-pods.<identifier>`](#settingskubernetesstatic-podsidentifier)
    - [`settings.kubernetes.standalone-mode`](#settingskubernetesstandalone-mode)
    - [`settings.kubernetes.system-reserved`](#settingskubernetessystem-reserved)
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

# Additional Resources

- **Kubernetes Official Documentation**: [Kubelet TLS Bootstrapping](https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet-tls-bootstrapping/)
- **Exoscale Cloud Controller Manager**: [GitHub Repository](https://github.com/exoscale/exoscale-cloud-controller-manager)
- **Bottlerocket Settings**: [Bottlerocket OS Settings](https://bottlerocket.dev/docs/settings)
