# E2E Testing for Datum DNS Webhook

End-to-end tests validate the datum-dns-webhook provider integration with ExternalDNS and the Datum DNS Operator.

## Quick Start

### Prerequisites

- Docker
- [Task](https://taskfile.dev) - Task runner
- [Chainsaw](https://kyverno.github.io/chainsaw/) - E2E testing tool

**Install Task:**

```bash
# macOS
brew install go-task/tap/go-task

# Linux
sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

# Or see https://taskfile.dev/installation/
```

**Install Chainsaw:**

```bash
# macOS
brew install kyverno/chainsaw/chainsaw

# Go install
go install github.com/kyverno/chainsaw@latest

# Or download binary from https://github.com/kyverno/chainsaw/releases
```

### Run Full E2E Suite

From the repository root:

```bash
task dev:setup
task test:e2e
```

This will:
1. Create a KIND cluster with base infrastructure (Flux, cert-manager, Envoy Gateway)
2. Build and load the webhook image
3. Deploy DNS operator (mock), fixtures, ExternalDNS, and test app
4. Run Chainsaw E2E tests

### Interactive Development

For iterative development and debugging:

```bash
# Setup environment (once)
task dev:setup

# In another terminal, export KUBECONFIG
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig

# View logs
task e2e:logs

# Run tests
task test:e2e

# Collect diagnostics
task e2e:diag

# When done
task e2e:cleanup
```

## Test Structure

### Configuration Layout

The E2E environment uses Kustomize for configuration management:

```
config/
├── base/                              # Webhook base deployment
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── rbac.yaml
│   └── kustomization.yaml
├── dependencies/
│   └── external-dns/                 # Flux HelmRelease for ExternalDNS
│       ├── helmrepository.yaml       # Points to external-dns chart repo
│       ├── helmrelease.yaml          # Configures webhook provider
│       └── kustomization.yaml
└── overlays/
    └── test/                         # E2E test overlay
        ├── dns-operator/             # Mock DNS operator
        ├── dns-fixtures/             # DNSZone, DNSZoneClass
        ├── test-app/                 # whoami + HTTPRoute
        └── kustomization.yaml        # Webhook + test app
```

This structure follows Kustomize best practices and mirrors production deployment patterns using Flux CD.

### Infrastructure Layers

1. **Base Infrastructure** (from test-infra)
   - KIND cluster
   - Flux CD
   - cert-manager
   - Envoy Gateway

2. **DNS Infrastructure** (this repo)
   - Mock DNS operator (marks zones/recordsets as ready)
   - DNSZoneClass and DNSZone fixtures

3. **Application Layer**
   - ExternalDNS (deployed via Flux HelmRelease)
   - datum-dns-webhook provider
   - Test app (whoami) with HTTPRoute

### What's Tested

Tests use [Chainsaw](https://kyverno.github.io/chainsaw/) for declarative E2E testing that validates the full reconciliation flow:

1. **HTTPRoute Creates Record** - Create HTTPRoute → verify DNSRecordSet created → delete HTTPRoute → verify DNSRecordSet removed
2. **TXT Ownership Record** - Verify ExternalDNS creates TXT records for ownership tracking alongside A records

See `chainsaw/README.md` for detailed test documentation.

## Available Tasks

Run from repository root:

| Task | Description |
|------|-------------|
| `task dev:setup` | Create complete E2E environment for development |
| `task test:e2e` | Run E2E tests (requires environment setup) |
| `task e2e:build` | Build and load webhook image into KIND |
| `task e2e:deploy` | Deploy all components (operator, webhook, test app) |
| `task e2e:logs` | Show logs from webhook and ExternalDNS |
| `task e2e:diag` | Collect full diagnostics to e2e/e2e-diagnostics/ |
| `task e2e:cleanup` | Delete KIND cluster and kubeconfig |
| `task ci:e2e` | Run full CI suite (setup + test + diagnostics on failure) |

## CI Integration

The `.github/workflows/e2e.yaml` workflow runs on every PR and push to main. It executes `task ci:e2e` which:

- Creates KIND cluster with base infrastructure
- Builds and loads the webhook image
- Deploys all test components
- Runs Chainsaw tests
- Collects diagnostics on failure (uploaded as artifacts)

## Troubleshooting

### Webhook not ready

```bash
task e2e:logs
# or
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig
kubectl logs -n external-dns -l app=datum-dns-webhook
kubectl describe pod -n external-dns -l app=datum-dns-webhook
```

### DNSRecordSet not created

Check ExternalDNS logs:
```bash
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig
kubectl logs -n external-dns -l app=external-dns
```

Check webhook logs:
```bash
kubectl logs -n external-dns -l app=datum-dns-webhook
```

### Full diagnostics

```bash
task e2e:diag
```

Outputs all logs, resources, and metrics to `./e2e/e2e-diagnostics/`.

## Local Development with test-infra

The root Taskfile includes test-infra remotely from GitHub. To use a local clone of test-infra for development:

```bash
# Clone test-infra next to this repository
cd ..
git clone https://github.com/datumforge/test-infra

# Set environment variable to use local version
export TASKFILE_TESTINFRA_PATH=../test-infra/Taskfile.yml

# Run tasks as normal
cd datum-dns-webhook
task dev:setup
```

## Test Implementation

Tests are written in declarative YAML using [Chainsaw](https://kyverno.github.io/chainsaw/):

- **Lifecycle testing**: Tests the full create → verify → delete → verify flow
- **Clear assertions**: YAML structure shows exactly what's being validated
- **Better debugging**: Chainsaw shows exactly which assertion failed
- **CI integration**: JUnit XML output for GitHub Actions

See `e2e/TESTING.md` for detailed testing reference.
