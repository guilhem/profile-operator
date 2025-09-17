# Profile Operator

A Kubernetes operator that enables declarative configuration management through reusable profiles. Apply consistent configuration templates across multiple resources using strategic merge patches or server-side apply.

## Overview

The Profile Operator provides two Custom Resource Definitions (CRDs):

- **Profile**: Cluster-scoped templates containing reusable configuration overlays
- **ProfileBinding**: Namespace-scoped resources that apply profiles to target resources

This allows you to separate configuration policy from resource selection, making it easy to apply consistent settings across multiple workloads.

## Quick Start

### Install the Operator

```bash
# Apply the CRDs and operator
kubectl apply -f https://github.com/guilhem/profile-operator/releases/latest/download/install.yaml
```

### Create a Profile

```yaml
apiVersion: profiles.barpilot.io/v1alpha1
kind: Profile
metadata:
  name: security-defaults
spec:
  description: "Apply security best practices to deployments"
  template:
    patchStrategicMerge:
      metadata:
        labels:
          security.barpilot.io/hardened: "true"
      spec:
        template:
          spec:
            securityContext:
              runAsNonRoot: true
              runAsUser: 1000
            containers:
            - name: app
              securityContext:
                allowPrivilegeEscalation: false
                readOnlyRootFilesystem: true
                capabilities:
                  drop: ["ALL"]
```

### Create a ProfileBinding

```yaml
apiVersion: profiles.barpilot.io/v1alpha1
kind: ProfileBinding
metadata:
  name: apply-security-to-apps
  namespace: production
spec:
  profileRef:
    name: security-defaults
  targetSelector:
    resourceRule:
      apiGroups: ["apps"]
      apiVersions: ["v1"]
      resources: ["deployments"]
    objectSelector:
      matchLabels:
        tier: frontend
    namespaces:
    - production
    - staging
```

## Key Features

- **Declarative Configuration**: Define reusable configuration templates as Kubernetes resources
- **Flexible Targeting**: Use label selectors and resource rules to target specific workloads
- **Strategic Merge**: Apply configuration overlays using Kubernetes strategic merge patches
- **Server-Side Apply**: Optional SSA support for advanced use cases
- **Namespace Isolation**: ProfileBindings are namespace-scoped for security
- **Status Tracking**: Monitor which resources are targeted, updated, and failed

## Use Cases

- **Security Hardening**: Apply consistent security policies across workloads
- **Resource Limits**: Enforce CPU/memory limits organization-wide  
- **Image Management**: Update container images across multiple deployments
- **Configuration Injection**: Add common ConfigMap/Secret mounts
- **Label Management**: Ensure consistent labeling for monitoring and governance

## API Reference

### Profile

- **Scope**: Cluster
- **Purpose**: Define reusable configuration templates

Key fields:
- `spec.template.patchStrategicMerge`: Configuration overlay to apply
- `spec.applyStrategy`: How to apply the profile (`SSA` or `Patch`)
- `spec.description`: Human-readable description

### ProfileBinding  

- **Scope**: Namespace
- **Purpose**: Apply profiles to target resources

Key fields:
- `spec.profileRef.name`: Name of the Profile to apply
- `spec.targetSelector.resourceRule`: Which resource types to target
- `spec.targetSelector.objectSelector`: Label selector for specific objects
- `spec.targetSelector.namespaces`: Target namespaces (defaults to binding's namespace)
- `spec.updateStrategy.type`: Update rollout strategy (currently `Immediate`)

## Development

### Prerequisites

- Go 1.24+
- Kubernetes 1.30+
- kubectl
- make

### Build and Test

```bash
# Install dependencies
make deps

# Run tests
make test

# Build operator
make build

# Generate CRDs and code
make generate manifests

# Run locally (requires kubeconfig)
make run
```

### Deploy to Development Cluster

```bash
# Build and load image
make docker-build docker-push IMG=your-registry/profile-operator:latest

# Deploy to cluster
make deploy IMG=your-registry/profile-operator:latest

# Undeploy
make undeploy
```

## Examples

See the [config/samples/](./config/samples/) directory for more examples:

- [Basic Profile](./config/samples/profiles_v1alpha1_profile.yaml)
- [ProfileBinding Example](./config/samples/profiles_v1alpha1_profilebinding.yaml)
- [ConfigMap Profile](./config/samples/configmap-profile.yaml)
- [Additional Examples](./config/samples/additional-examples.yaml)

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make changes and add tests
4. Run `make test lint`
5. Submit a pull request

## License

Licensed under the Apache License, Version 2.0.
