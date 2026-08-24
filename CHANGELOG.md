Changelog
=========

1.36.3
----------

- feat(instance): automatically attach sks cluster default securitygroup (when env-var is set)
- fix(deps): bump golang 1.26.6
- fix: ExoscaleNodeClass `spec.kubelet` definition covering default values (!158)
- fix(deps): Update kubernetes monorepo to v0.36.4 (#157) [GitHub]
- fix(deps): Update github.com/awslabs/operatorpkg digest to 6d329ce (#153)
- fix(deps): Update module github.com/stretchr/testify to v1.12.1 (#155)
- fix(deps): Update module github.com/exoscale/egoscale/v3 to v3.1.46 (#156)
- fix(deps): Update module sigs.k8s.io/karpenter to v1.14.1 (#160)

1.36.2
------------------

- fix(rbac): allow pod deletion (after an eviction was attempted and failed)
- feat: add support for EIP attachment to instances
- fix(deps): update module github.com/exoscale/egoscale/v3 to v3.1.43 (#135)
- fix(deps): update golang docker tag to v1.26.5 (#146)
- fix(deps): update go-toml to v2.4.3 (#140)
- feat(deps): update module sigs.k8s.io/karpenter to v1.14.0 (#134)
- fix(deps): update module sigs.k8s.io/controller-runtime to v0.24.1 (#99)
- feat(perf): filter listed instances when calling compute API (#150)
- feat: add container-registry settings to NodeClass spec (#151)
- fix: instance template ID drift detection now works properly (#151)
- feat: add kubelet CPU manager settings (#151)

1.36.1
------------------

- feat: add support for custom user-data configuration (#122)
- fix(drift): skip drift detection for instances that are still provisioning
- fix(gc): do not delete instances that are still being provisioned
- fix(node): make Node pre-creation idempotent to avoid "node already exists" failures
- fix(deps): update module sigs.k8s.io/karpenter to v1.12.1 (#137)
- fix(deps): update module github.com/exoscale/egoscale/v3 to v3.1.37 (#137)

1.36.0
------------------

- feat: add compute instances IPv6 support
- fix(deps): update module sigs.k8s.io/karpenter to v1.12.0 (#130)
- fix(deps): update module github.com/exoscale/egoscale/v3 to v3.1.35 (#128)
- fix(deps): update module github.com/pelletier/go-toml/v2 to v2.3.1 (#131)

1.35.0
------------------

- docs: add upgrade notes for 1.0.0
- chore(deps): update golang docker tag to v1.26.2 (#124)
- fix(deps): update module github.com/pelletier/go-toml/v2 to v2.3.0 (#119)
- fix(deps): update kubernetes monorepo to v0.35.3 (#117)
- fix(deps): update module sigs.k8s.io/karpenter to v1.10.0 (#118)
- fix(deps): update module github.com/samber/lo to v1.53.0 (#111)
- fix(deps): update module github.com/exoscale/egoscale/v3 to v3.1.34 (#116)
- fix(nodeclass): scope orphan cleanup to cluster ID (#126)
- fix(deps): update module sigs.k8s.io/karpenter to v1.11.1 (#123)
- fix(deps): update kubernetes monorepo to v0.35.4 (#127)
- align karpenter version with currently latest supported Kubernetes minor version (1.35)

1.0.0
------------------

- bump to major version
- node: set Hostname, ExternalIP and InternalIP addresses on Node objects based on instance metadata
- fix(deps): update module sigs.k8s.io/karpenter to v1.9.0 (#105)
- chore(deps): update golang docker tag to v1.26.0 (#103)

0.0.13
----------

- exoscalenodeclass: use multiple conditions to report ready state

0.0.12
----------

- rbac: add missing permissions
- ci: wrong build date (using month for minutes)
- add support for feature gates
- add nodeclass privateNetwork, anti-affinity groups & securityGroups selectorTerms & status

0.0.11
----------

- cloudprovider: record events when drift is detected on NodeClaims
- provider: add nodepool name label to instances
- cloudprovider: auto-update instance labels when drift is detected
- chore: bump to go 1.25.5*
- cloudprovider: better interactions with karpenter framework
- cloudprovider: provision node object early to prevent VM dupes on rapid scale-up

0.0.10
----------

- doc: Add documentation regarding SKS image metadata format
- **Breaking change**: Move kubelet configuration to `spec.kubelet` (imageGC*, kubeReserved, systemReserved)
- **Breaking change**: Remove deprecated `spec.nodeLabels` and `spec.nodeTaints` fields
- Add `spec.kubelet.clusterDNS` with default `["10.96.0.10"]`
- fix: ephemeral storage reporting

0.0.9
----------

- Switch instance label back to the expected namespaced format

0.0.8
----------

- Re-introduce conservative node-drain on NodeClaim deletion

0.0.7
----------

- fix: broken preprod API endpoint support

0.0.6
----------

- Overhaul of the codebase
- fix: overprovisioning due to missing labels on NodeClaims
- fix: missing rbac rules and manifest

0.0.5
----------

- fix: idempotent instance deletion
- fix: missing template ID in NodeClaims
- fix: missing default resource overhead preventing correct provisioning
- fix: default node prefix now empty string

0.0.4
----------

- Support custom cluster endpoint (falling back to client configuration host)

0.0.3
----------

- Make ExoscaleNodeClass templateID mutable
- Support node OS template selection with imageTemplateSelector {}
- Support preprod API endpoint
- chore(deps): update golang docker tag to v1.25.1
- fix(deps): update kubernetes packages to v0.34.1
- fix(deps): update module sigs.k8s.io/controller-runtime to v0.22.1
- fix(deps): update module sigs.k8s.io/karpenter to v1.7.1
- fix(cloudprovider): drain nodes with a timeout on Delete

0.0.2
----------

- Add Karpenter deployment manifests
- provider: drop clusterName (unused attribute)
- cloudprovider: use clusterID instead of clusterName
- Add EXOSCALE_COMPUTE_INSTANCE_PREFIX configuration

0.0.1
------

- Initial release
