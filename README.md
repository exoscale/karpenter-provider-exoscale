# Karpenter Provider for Exoscale

[Karpenter](https://karpenter.sh/) provider implementation for [Exoscale](https://www.exoscale.com/) cloud platform, enabling efficient Kubernetes node autoscaling.

## Overview

This provider enables Karpenter to provision and manage Exoscale compute instances directly, supporting both SKS (Scalable Kubernetes Service) and self-managed Kubernetes clusters. It automatically selects the most cost-effective instance types based on workload requirements.

## Key Features

- **Direct instance provisioning** with automatic cost optimization
- **Drift detection** for templates, security groups, networks, and anti-affinity groups  
- **Self-healing** with node repair policies
- **Secure bootstrapping** using temporary tokens with automatic cleanup
- **Full Exoscale integration** including private networks and anti-affinity groups
- **Dynamic instance discovery** with built-in pricing data

## Architecture

- **Controllers**: NodeClass, NodeClaim, Bootstrap Token, Repair, Garbage Collection
- **Providers**: Instance, Instance Type, Pricing, User Data
- **Features**: Drift detection, self-healing, secure bootstrapping, cost optimization

## Configuration

Karpenter Exoscale implementation requires some configuration to work properly.

Here is the required environment variables:
* `EXOSCALE_SKS_CLUSTER_ID`: unique identifier (UUID) of your Kubernetes cluster. It will be used to filter nodes in Exoscale APIs.
* `EXOSCALE_COMPUTE_INSTANCE_PREFIX`: prefix used to name instances created by Karpenter. Defaults to `karpenter`.
* `CLUSTER_DNS_IP`: DNS IP of your Kubernetes cluster. It will be setup in Kubelet configuration.
* `CLUSTER_DOMAIN`: Domain name of your Kubernetes cluster. It will be setup in Kubelet configuration.
* `EXOSCALE_API_KEY`: Your Exoscale API key
* `EXOSCALE_API_SECRET`: Your Exoscale API secret
* `EXOSCALE_ZONE`: SKS zone hosting your SKS cluster

Only if out-of-cluster:
* `KUBECONFIG`: Path to your kubeconfig file. You can extract it from your cluster view in our
  [console](https://portal.exoscale.com)


## In-Cluster CRDs

If you are deploying Karpenter Exoscale provider yourself, you'll need to install the required CRDs:

```bash
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/refs/tags/v1.7.1/pkg/apis/crds/karpenter.sh_nodeclaims.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/refs/tags/v1.7.1/pkg/apis/crds/karpenter.sh_nodepools.yaml
kubectl apply -f https://raw.githubusercontent.com/kubernetes-sigs/karpenter/refs/tags/v1.7.1/pkg/apis/crds/karpenter.sh_nodeoverlays.yaml
```

## NodeClasses and NodeClaims

Karpenter uses NodeClasses and NodeClaims to define and manage the desired state of nodes in the cluster.

- **NodeClass**: A NodeClass defines a set of requirements for a group of nodes, such as instance type, disk size, and network configuration. It acts as a template for creating nodes.
- **NodeClaim**: A NodeClaim represents a request for a specific node to be created. It references a NodeClass and includes additional information such as workload requirements and user data.

> **📁 See [examples/](examples/) for complete sample configurations including GPU setups and imageTemplateSelector usage.**

Here is an example NodeClass for regular compute:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: standard
spec:
  # Template ID for the instance (required)
  # This should be a valid Exoscale template ID from your zone
  # If not set, you can use imageTemplateSelector instead or it will
  # default to the template matching apiserver version
  templateID: "<setme>"
  
  # Disk size in GB (default: 50, min: 10, max: 8000)
  diskSize: 100
  
  # Security groups (optional)
  # List the security group IDs to attach to instances
  securityGroupsSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-security-group"

  # Anti-affinity groups (optional)
  antiAffinityGroupsSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-anti-affinity-group"

  # Private networks (optional)
  privateNetworksSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-private-network"

  # Kubelet exposed configuration (all those parameters are optional)
  # https://kubernetes.io/docs/reference/command-line-tools-reference/kubelet/
#   imageGCHighThresholdPercent: 80
#   imageGCLowThresholdPercent: 70
#   imageMinimumGCAge: 5m
#   kubeReserved:
#     cpu: "100m"
#     memory: "256Mi"
#     ephemeralStorage: "1Gi"
#   systemReserved:
#     cpu: "100m"
#     memory: "256Mi"
#     ephemeralStorage: "1Gi"

  # Node labels (optional)
  # These labels will be applied to all nodes using this NodeClass
  # nodeLabels:
  #   environment: "production"
  #   team: "platform"
  
  # Node taints (optional)
  # These taints will be applied to all nodes using this NodeClass
  # nodeTaints:
  #   - key: "dedicated"
  #     value: "gpu"
  #     effect: "NoSchedule"
```

If you want GPU nodes, here is an example NodeClass for GPU instances:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: gpu
spec:
  # Template ID for GPU instances (required)
  # Replace with actual GPU-enabled template UUID
  templateID: "<setme>"
  
  # Minimal disk to save costs
  diskSize: 50  # Minimum viable disk size
  
  securityGroups: []
  
  # Anti-affinity groups for spreading GPU nodes
  antiAffinityGroups: []

  privateNetworks: []
  
  # GPU-specific labels
  nodeLabels:
    workload-type: "gpu"
    gpu-enabled: "true"
    environment: "production"
    "nvidia.com/gpu.present": "true"
  
  # GPU-specific taints
  nodeTaints:
    - key: "nvidia.com/gpu"
      value: "true"
      effect: "NoSchedule"
```

Instead of specifying a concrete `templateID`, you can use `imageTemplateSelector` to select an OS image template
based on the Kubernetes cluster version and optional `variant` (for example `nvidia` for GPU optimized images).

Fields:

- `version` (optional) — a semver string like `1.34.1` (`major.minor.patch`). If omitted (or if you set `imageTemplateSelector: {}`), 
  Karpenter will automatically detect the control plane Kubernetes version at runtime when resolving the template.
- `variant` (optional) — a string such as `standard` or `nvidia`. Defaults to `standard` when not set.

Validation: the CRD enforces that exactly one of `templateID` or `imageTemplateSelector` must be set on an
`ExoscaleNodeClass` (see `apis/karpenter/v1/exoscalenodeclass_types.go`).

Example using `imageTemplateSelector` with explicit version:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: standard-latest
spec:
  imageTemplateSelector:
    version: "1.34.1"
    variant: "standard"
  diskSize: 50
  securityGroups: []
  privateNetworks: []
```

Example using `imageTemplateSelector: {}` to auto-detect control plane version:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: standard-auto
spec:
  # Empty imageTemplateSelector automatically uses the control plane K8s version
  imageTemplateSelector: {}
  diskSize: 50
  securityGroups: []
  privateNetworks: []
```

When `imageTemplateSelector` is used the provider will resolve the optimal template ID for the given `version`
and `variant` at provisioning time (see `pkg/providers/template/resolver.go`).

When ExoscaleNodeClasses are defined you can now create NodeClaims that reference them:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: standard
spec:
  template:
    metadata:
      labels:
        nodepool: standard
    spec:
      nodeClassRef:
        group: karpenter.exoscale.com
        kind: ExoscaleNodeClass
        name: standard
      

      startupTaints:
        - key: karpenter.sh/unregistered
          effect: NoExecute
      
      requirements:
        - key: "node.kubernetes.io/instance-type"
          operator: In
          values:
            # - "standard.small"
            - "standard.medium"
            - "standard.large"
            - "standard.extra-large"

      expireAfter: 30m
  
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    
    consolidateAfter: 30m
    
    budgets:
    - nodes: "10%"  # Disrupt at most 10% of nodes at once

  weight: 50
```

Here is a GPU one:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: gpu
spec:
  template:
    metadata:
      labels:
        nodepool: gpu
        workload-type: gpu
    spec:
      nodeClassRef:
        group: karpenter.exoscale.com
        kind: ExoscaleNodeClass
        name: gpu

      startupTaints:
        - key: karpenter.sh/unregistered
          effect: NoExecute

      requirements:
        - key: "node.kubernetes.io/instance-type"
          operator: In
          values:
            - "gpua30.small"
            - "gpua30.medium"
            - "gpua30.large"

      taints:
        - key: "nvidia.com/gpu"
          value: "true"
          effect: "NoSchedule"
      
      expireAfter: 30m
  
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized

    consolidateAfter: 1m
    
    budgets:
    - nodes: "100%"

  weight: 10
```

## License

Apache License 2.0
