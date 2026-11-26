# Karpenter Provider for Exoscale

[Karpenter](https://karpenter.sh/) provider for
[Exoscale](https://www.exoscale.com/). It provisions Exoscale compute instances
for SKS (Scalable Kubernetes Service) and self-managed Kubernetes clusters,
picking the cheapest instance type that fits pending workloads.

## Configuration

Required environment variables:
* `EXOSCALE_SKS_CLUSTER_ID`: unique identifier (UUID) of your Kubernetes
  cluster. It will be used to filter nodes in Exoscale APIs.
* `EXOSCALE_API_KEY`: Your Exoscale API key
* `EXOSCALE_API_SECRET`: Your Exoscale API secret
* `EXOSCALE_ZONE`: Exoscale zone hosting your cluster

Optional environment variables:
* `EXOSCALE_COMPUTE_INSTANCE_PREFIX`: prefix used to name instances created by
Karpenter. Empty by default: instances are named after their NodeClaim.
* `CLUSTER_DOMAIN`: Domain name of your Kubernetes cluster. It will be set in
Kubelet configuration. Defaults to `cluster.local`.
* `CLUSTER_ENDPOINT`: Kubernetes API server endpoint that new nodes join.
Defaults to the API server of the kubeconfig in use. Set it when running
in-cluster: the in-cluster address is a service IP that nodes can't reach before
they joined.
* `EXOSCALE_API_ENDPOINT`: Custom Exoscale API endpoint URL. If not set, the
endpoint is auto-detected from the zone.
* `EXOSCALE_API_ENVIRONMENT`: Exoscale API environment (e.g., `ppapi` for
pre-production). If not set, the production environment is used.
* `USE_SKS_DEFAULT_SECURITY_GROUP`: Attach node to parent cluster default
security group. Defaults to `'false'` when not set or different from `'true'`.

> [!IMPORTANT]
> The IAM role bound to `EXOSCALE_API_KEY` must allow every API operation the
> provider invokes. See [Required IAM policy](doc/iam.md).

Only if out-of-cluster:
* `KUBECONFIG`: Path to your kubeconfig file, downloadable from the cluster view in the
  [console](https://portal.exoscale.com).

## Self-hosted installation

On SKS, enabling the `karpenter` addon installs and runs the provider for you.
To run it yourself instead, install the CRDs, create the Secret the Deployment
reads its settings from, then apply the manifests in [`config/`](config/):

```bash
kubectl apply -k config/crd/bases
kubectl -n kube-system create secret generic karpenter-exoscale \
  --from-literal=api-key=EXO... \
  --from-literal=api-secret=... \
  --from-literal=zone=ch-gva-2 \
  --from-literal=cluster-id=<cluster UUID> \
  --from-literal=cluster-endpoint=https://<cluster endpoint> \
  --from-literal=use-sks-default-security-group=true   # optional, SKS only
kubectl apply -k config
```

The API key needs the [IAM policy](doc/iam.md). `config/servicemonitor.yaml` and
`config/verticalpodautoscaler.yaml` are optional: they need the Prometheus
operator and the VPA CRDs.

## NodeClasses and NodeClaims

- **ExoscaleNodeClass** holds the Exoscale-specific node configuration: template,
  disk size, networking, kubelet settings. Full reference:
  [doc/nodeclass.md](doc/nodeclass.md).
- **NodePool** (Karpenter core) references an ExoscaleNodeClass and sets
  instance-type requirements, labels, taints and disruption policy. Karpenter
  creates one **NodeClaim** per node it launches.

More samples, including GPU setups, are in [examples/](examples/).

Example NodeClass:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: standard
spec:
  # Exoscale template UUID from your zone. Exactly one of templateID or
  # imageTemplateSelector must be set.
  templateID: "<setme>"

  # Disk size in GB (default: 50, min: 10, max: 8000)
  diskSize: 100

  # Security groups (optional)
  securityGroupSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-security-group"

  # Anti-affinity groups (optional)
  antiAffinityGroupSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-anti-affinity-group"

  # Private networks (optional, deprecated: use vpcSubnetSelectorTerms instead)
  privateNetworkSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-private-network"

  # VPC holding the subnets below, by ID or name.
  # Required when vpcSubnetSelectorTerms is set.
  # vpcSelector:
  #   name: "my-vpc"

  # VPC subnets (optional, max 7)
  # Subnets of the selected VPC, attached to instances at creation time.
  vpcSubnetSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "my-subnet"

  # IP forwarding (optional): let instances act as a router in their VPC
  # (e.g. NAT gateway). Immutable after creation; toggling triggers drift.
  # enableIPForwarding: false

  # Elastic IPs (optional)
  elasticIPSelectorTerms: []
  # - id: "123e4567-e89b-12d3-a456-426614174000"
  # - name: "203.0.113.42"   # matched against the EIP public address

  # Kubelet settings (optional), see doc/nodeclass.md#speckubelet
  # kubelet:
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
```

Node labels and taints are set on the NodePool, not the NodeClass.

Instead of a fixed `templateID`, `imageTemplateSelector` picks the SKS template
matching a Kubernetes `version` (`major.minor.patch`) and `variant` (`standard`,
the default, or `nvidia`). With `version` omitted, as in
`imageTemplateSelector: {}`, the control plane version is detected at runtime:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: gpu
spec:
  imageTemplateSelector:
    variant: nvidia
```

A NodePool using the `standard` NodeClass:

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

A GPU NodePool tainting its nodes so only GPU workloads land there:

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

## Upgrading

See [UPGRADE.md](UPGRADE.md) for version-specific migration instructions.

## License

Apache License 2.0
