Changelog
=========

UNRELEASED
------------------

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
