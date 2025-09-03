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
* `CLUSTER_DNS_IP`: DNS IP of your Kubernetes cluster. It will be setup in Kubelet configuration.
* `CLUSTER_DOMAIN`: Domain name of your Kubernetes cluster. It will be setup in Kubelet configuration.
* `EXOSCALE_API_KEY`: Your Exoscale API key
* `EXOSCALE_API_SECRET`: Your Exoscale API secret
* `EXOSCALE_ZONE`: SKS zone hosting your SKS cluster

Only if out-of-cluster:
* `KUBECONFIG`: Path to your kubeconfig file. You can extract it from your cluster view in our
  [console](https://portal.exoscale.com)

## License

Apache License 2.0
