# Upgrade Guide

## v1.0.0

### Breaking changes: selector terms for security groups, anti-affinity groups, and private networks

The fields `securityGroups`, `antiAffinityGroups`, and `privateNetworks` in `ExoscaleNodeClassSpec` have been
**deprecated** and replaced by selector terms fields that allow selecting resources either by ID or by name:

| Deprecated field       | Replacement field                      |
|------------------------|----------------------------------------|
| `securityGroups`       | `securityGroupSelectorTerms`           |
| `antiAffinityGroups`   | `antiAffinityGroupSelectorTerms`       |
| `privateNetworks`      | `privateNetworkSelectorTerms`          |

Each selector term accepts exactly one of `id` or `name`.

The deprecated fields will be removed in a future release.

#### Migration

**Before (deprecated):**

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: standard
spec:
  imageTemplateSelector: {}
  securityGroups:
    - "123e4567-e89b-12d3-a456-426614174000"
    - "234e5678-e89b-12d3-a456-426614174001"
  antiAffinityGroups:
    - "345e6789-e89b-12d3-a456-426614174002"
  privateNetworks:
    - "456e7890-e89b-12d3-a456-426614174003"
```

**After:**

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: standard
spec:
  imageTemplateSelector: {}
  securityGroupSelectorTerms:
    - id: "123e4567-e89b-12d3-a456-426614174000"
    - id: "234e5678-e89b-12d3-a456-426614174001"
  antiAffinityGroupSelectorTerms:
    - id: "345e6789-e89b-12d3-a456-426614174002"
  privateNetworkSelectorTerms:
    - id: "456e7890-e89b-12d3-a456-426614174003"
```

Resources can also be referenced by name instead of ID:

```yaml
  securityGroupSelectorTerms:
    - name: "my-security-group"
  antiAffinityGroupSelectorTerms:
    - name: "my-anti-affinity-group"
  privateNetworkSelectorTerms:
    - name: "my-private-network"
```

> **Note:** Each selector term must specify exactly one of `id` or `name`. Specifying both or neither is invalid.
