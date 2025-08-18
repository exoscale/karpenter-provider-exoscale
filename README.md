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

## License

Apache License 2.0
