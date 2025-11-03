Changelog
=========

0.0.6
----------

- Overhaul of the codebase
- fix: overprovisioning due to missing labels on NodeClaims

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
