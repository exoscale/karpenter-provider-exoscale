Changelog
=========

Unreleased
----------

- Make ExoscaleNodeClass templateID mutable
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
