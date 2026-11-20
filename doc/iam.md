# Required Exoscale IAM Policy

The `EXOSCALE_API_KEY` consumed by karpenter-exoscale must be bound to an
[Exoscale IAM Role](https://community.exoscale.com/product/security/iam/) that
allows every API operation the provider invokes on the `compute` service.

The exact operation names below are those sent on the wire by the
[`egoscale/v3`](https://github.com/exoscale/egoscale) SDK and validated by the
IAM engine.

## Operations used by karpenter-exoscale

| Resource         | Operations                                                                                                    |
|------------------|---------------------------------------------------------------------------------------------------------------|
| Instance         | `list-instances`, `get-instance`, `create-instance`, `delete-instance`, `update-instance`, `reboot-instance`  |
| Instance Type    | `list-instance-types` (refreshed at startup)                                                                  |
| Template         | `get-active-nodepool-template`, `get-template`                                                                |
| Security Group   | `list-security-groups`, `get-security-group`                                                                  |
| Anti-Affinity    | `list-anti-affinity-groups`, `get-anti-affinity-group`                                                        |
| Private Network  | `list-private-networks`, `get-private-network`, `attach-instance-to-private-network`                          |
| Elastic IP       | `list-elastic-ips`, `get-elastic-ip`, `attach-instance-to-elastic-ip`                                         |
| VPC              | `list-vpcs`, `get-vpc`                                                                                        |
| VPC Subnet       | `list-subnets`, `get-subnet`                                                                                  |
| SKS Cluster      | `get-sks-cluster`                                                                                             |
| Zone             | `list-zones`                                                                                                  |
| Operations poll  | `get-operation` (called by the SDK to wait for async operations to complete)                                  |


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
          "expression": "operation in ['create-instance','delete-instance','get-active-nodepool-template','get-anti-affinity-group','get-instance','get-operation','get-template','get-private-network','get-elastic-ip','get-security-group','get-sks-cluster','list-anti-affinity-groups','list-instances','list-instance-types','list-private-networks','list-security-groups','list-elastic-ips','list-zones','attach-instance-to-private-network','attach-instance-to-elastic-ip','get-vpc','list-vpcs','get-subnet','list-subnets','reboot-instance','update-instance']"
        }
      ]
    }
  }
}
```

The `default-service-strategy` is set to `deny` so any operation not explicitly
allowed above (including DNS, DBaaS, object storage, IAM, etc.) is rejected.

Notes:

- `update-instance` is used to re-apply Karpenter-managed labels to existing
  instances when label drift is detected.
- `reboot-instance` is required by the Karpenter v1.15 `CloudProvider` interface
  (`Reboot`).
- `get-vpc`, `list-vpcs`, `get-subnet` and `list-subnets` are required when the
  NodeClass uses `spec.vpcSelector` / `spec.vpcSubnetSelectorTerms`; instances
  are attached to the resolved subnets at creation time via `create-instance`.
