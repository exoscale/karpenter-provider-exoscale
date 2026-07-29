# Required Exoscale IAM Policy

The `EXOSCALE_API_KEY` consumed by karpenter-exoscale must be bound to an
[Exoscale IAM Role](https://community.exoscale.com/product/security/iam/) that
allows every API operation the provider invokes on the `compute` service.

The exact operation names below are those sent on the wire by the
[`egoscale/v3`](https://github.com/exoscale/egoscale) SDK and validated by the
IAM engine.

## Operations used by karpenter-exoscale

| Resource         | Operations                                                                                    |
|------------------|-----------------------------------------------------------------------------------------------|
| Instance         | `list-instances`, `get-instance`, `create-instance`, `delete-instance`, `update-instance`     |
| Instance Type    | `list-instance-types` (refreshed at startup)                                                  |
| Template         | `list-templates`, `get-template`                                                              |
| Security Group   | `list-security-groups`, `get-security-group`                                                  |
| Anti-Affinity    | `list-anti-affinity-groups`, `get-anti-affinity-group`                                        |
| Private Network  | `list-private-networks`, `get-private-network`, `attach-instance-to-private-network`          |
| Elastic IP       | `list-elastic-ips`, `get-elastic-ip`, `attach-instance-to-elastic-ip`                         |
| Operations poll  | `get-operation` (called by the SDK to wait for async operations to complete)                  |


## Least-privilege IAM policy

The following policy can be applied as an 
[Organization Policy](https://community.exoscale.com/product/security/iam/how-to/policy-guide/)
or attached to an IAM Role bound to the API key:

```json
{
  "default-service-strategy": "deny",
  "services": {
    "compute": {
      "type": "rules",
      "rules": [
        {
          "action": "allow",
          "expression": "operation in ['create-instance','delete-instance','get-active-nodepool-template','get-anti-affinity-group','get-instance','get-operation','get-template','get-private-network','get-elastic-ip','get-security-group','list-anti-affinity-groups','list-instances','list-instance-types','list-private-networks','list-security-groups','list-elastic-ips','list-zones','attach-instance-to-private-network','attach-instance-to-elastic-ip']"
        }
      ]
    }
  }
}
```

The `default-service-strategy` is set to `deny` so any operation not explicitly
allowed above (including DNS, DBaaS, object storage, IAM, etc.) is rejected.

Note: `update-instance` is **not** in the policy above on purpose — the provider
only uses it to re-apply Karpenter-managed labels to existing instances, and
even then it tolerates failures. If your environment requires this
self-healing, add `'update-instance'` to the array.
