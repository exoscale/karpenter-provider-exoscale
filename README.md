# Karpenter Provider for Exoscale

A [Karpenter](https://karpenter.sh/) provider implementation for [Exoscale](https://www.exoscale.com/),
enabling efficient Kubernetes node autoscaling on Exoscale cloud platform.

## Overview

This provider enables Karpenter to manage compute instances directly on Exoscale,
supporting both SKS (Scalable Kubernetes Service) and self-managed Kubernetes clusters.
It implements the full Karpenter provider interface with Exoscale-specific optimizations.

## Features

### Core Capabilities
- **Cluster-agnostic deployment**: Works seamlessly with SKS clusters and self-managed Kubernetes on Exoscale
- **Direct instance lifecycle management**: Provisions and manages compute instances without intermediate abstractions
- **Dynamic instance type selection**: Automatically selects optimal instance types based on workload requirements
- **Comprehensive drift detection**: Monitors and corrects configuration drift for templates, security groups, networks, and anti-affinity groups
- **Self-healing capabilities**: Automatically repairs unhealthy nodes based on configurable policies
- **Secure node bootstrapping**: Implements secure token-based node joining with automatic cleanup

### Exoscale Platform Integration
- **Security groups**: Full support for Exoscale security group configuration
- **Anti-affinity groups**: Instance placement constraints for high availability
- **Private networks**: Support for private network attachments
- **Template-based provisioning**: Uses Exoscale templates for consistent instance creation
- **Zone-aware operation**: Designed for Exoscale's zone architecture

## Requirements

- Kubernetes 1.25+ (for CEL validation support)
- Exoscale API credentials with compute instance management permissions
- Karpenter core components

## Installation

### Environment Configuration

Required environment variables:
```bash
export EXOSCALE_ZONE=ch-gva-2              # Target Exoscale zone
export CLUSTER_NAME=my-cluster             # Unique cluster identifier
export EXOSCALE_API_KEY=EXO...             # Exoscale API key
export EXOSCALE_API_SECRET=...             # Exoscale API secret
```

Optional configuration:
```bash
export CLUSTER_DNS_IP=10.96.0.10           # Cluster DNS service IP(s)
export CLUSTER_DOMAIN=cluster.local        # Cluster domain
```

### Deployment

#### Using Docker
```bash
# Build the image
docker build -t karpenter-exoscale:latest .

# Deploy CRDs
kubectl apply -f config/crd/bases/

# Deploy the provider (ensure environment variables are configured)
kubectl apply -f config/manager/
```

#### Building from Source

```bash
# Install dependencies
go mod download

# Build the binary
make build

# Or directly
go build -o bin/karpenter-exoscale cmd/karpenter-exoscale/main.go
```

## Configuration

### ExoscaleNodeClass

The ExoscaleNodeClass custom resource defines Exoscale-specific configuration for node provisioning:

```yaml
apiVersion: karpenter.exoscale.com/v1
kind: ExoscaleNodeClass
metadata:
  name: default
spec:
  # Required: Exoscale template UUID
  templateID: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
  
  # Optional: Instance configuration
  diskSize: 100  # Root disk size in GB (default: 50)
  
  # Optional: Network and security configuration
  securityGroups:
    - "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
  
  antiAffinityGroups:
    - "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
  
  privateNetworks:
    - "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
  
  # Optional: Kubernetes configuration
  kubeReserved:
    cpu: "200m"
    memory: "300Mi"
    ephemeralStorage: "1Gi"
  
  systemReserved:
    cpu: "100m"
    memory: "100Mi"
    ephemeralStorage: "500Mi"
  
  # Optional: Node customization
  nodeLabels:
    environment: "production"
    team: "platform"
  
  nodeTaints:
    - key: "dedicated"
      value: "special-workload"
      effect: "NoSchedule"
  
  # Optional: Kubelet configuration
  imageGCHighThresholdPercent: 85
  imageGCLowThresholdPercent: 80
  imageMinimumGCAge: "5m"
```

### NodePool Configuration

Standard Karpenter NodePool resources work with ExoscaleNodeClass:

```yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default
spec:
  template:
    spec:
      nodeClassRef:
        group: karpenter.exoscale.com
        kind: ExoscaleNodeClass
        name: default
      
      requirements:
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand"]
        - key: node.kubernetes.io/instance-type
          operator: In
          values: 
            - standard.small
            - standard.medium
            - standard.large
  
  limits:
    cpu: "1000"
    memory: "1000Gi"
  
  disruption:
    consolidationPolicy: WhenEmpty
    expireAfter: 30m
```

## Architecture

### Controllers

The provider implements several controllers for comprehensive node management:

- **NodeClass Controller**: Validates ExoscaleNodeClass resources and manages their lifecycle
- **NodeClaim Controller**: Handles instance provisioning requests from Karpenter
- **Bootstrap Token Controller**: Manages secure token lifecycle for node joining
- **Repair Controller**: Implements self-healing for unhealthy nodes
- **Garbage Collection Controller**: Cleans up orphaned resources

### Provider Components

- **Instance Provider**: Direct integration with Exoscale compute API
- **Instance Type Provider**: Dynamic discovery of available instance types
- **Pricing Provider**: Cost information for optimization decisions
- **User Data Provider**: Generates cloud-init configuration for node bootstrap

## Monitoring and Observability

The provider exposes metrics for monitoring:

- Instance provisioning success/failure rates
- Bootstrap token operations
- Drift detection events
- API call metrics

Integrate with your existing Prometheus setup to collect these metrics.

## Development

### Building and Testing

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Generate code and manifests
make generate manifests

# Build binary
make build

# Build Docker image
make docker-build
```

### Project Structure

```
.
├── apis/karpenter/v1/        # API definitions
├── cmd/                      # Main application entry point
├── config/                   # Kubernetes manifests
├── internal/                 # Internal packages
├── pkg/
│   ├── cloudprovider/        # Core provider implementation
│   ├── controllers/          # Kubernetes controllers
│   └── providers/            # Exoscale-specific providers
└── test/                     # Test fixtures and utilities
```

## Support

For issues, questions, or contributions:
- Open an issue in the [GitHub repository](https://github.com/exoscale/karpenter-exoscale)
- Consult the [Karpenter documentation](https://karpenter.sh/) for general Karpenter concepts
- Contact Exoscale support for platform-specific questions

## License

Apache License 2.0 - See LICENSE file for details
