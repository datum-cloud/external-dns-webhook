# Deployment Configuration

This directory contains Kustomize configuration for deploying the datum-dns-webhook in various environments.

## Directory Structure

```
config/
├── base/                    # Base webhook deployment
├── dependencies/            # External dependencies
│   └── external-dns/       # Flux HelmRelease for ExternalDNS
└── overlays/               # Environment-specific overlays
    └── test/               # E2E test environment
```

## Base Configuration

The `base/` directory contains the core webhook deployment:

- `namespace.yaml` - external-dns namespace
- `serviceaccount.yaml` - Service account for webhook
- `rbac.yaml` - ClusterRole and ClusterRoleBinding
- `deployment.yaml` - Webhook deployment
- `service.yaml` - Webhook service (webhook and metrics ports)

## Dependencies

### external-dns

The `dependencies/external-dns/` directory contains Flux CD resources for deploying ExternalDNS:

- `helmrepository.yaml` - References the external-dns Helm chart repository
- `helmrelease.yaml` - Configures ExternalDNS to use the webhook provider
- `kustomization.yaml` - Kustomize base for the Flux resources

This deployment pattern mirrors production environments where Flux CD manages Helm releases.

## Overlays

### Test Overlay

The `overlays/test/` directory provides E2E testing configuration:

- `dns-operator/` - References e2e/manifests/dns-operator (mock DNS operator)
- `dns-fixtures/` - References e2e/manifests/dns-fixtures (DNSZone, DNSZoneClass)
- `test-app/` - References e2e/manifests/test-app (whoami + HTTPRoute)
- `kustomization.yaml` - Combines base webhook with test application

#### Usage

```bash
# Deploy DNS operator (mock)
kubectl apply -k config/overlays/test/dns-operator/

# Deploy DNS fixtures
kubectl apply -k config/overlays/test/dns-fixtures/

# Deploy ExternalDNS via Flux
kubectl apply -k config/dependencies/external-dns/
flux reconcile helmrelease external-dns -n external-dns

# Deploy webhook + test app
kubectl apply -k config/overlays/test/
```

Or use the automated tasks:

```bash
task e2e:deploy
```

## Validation

Verify all configurations build correctly:

```bash
# Base deployment
kubectl kustomize config/base/

# External DNS dependency
kubectl kustomize config/dependencies/external-dns/

# Test overlay
kubectl kustomize config/overlays/test/
```

## Production Deployment

For production environments, the webhook deployment follows the FluxCD pattern documented in the datum-cloud/infra repository:

1. Service publishes `config/` as OCI artifact to ghcr.io
2. Infra repo references OCI artifact via `OCIRepository`
3. Flux `Kustomization` resources apply configuration with environment-specific patches

See `.claude/skills/fluxcd-deployment/` for complete production patterns.
