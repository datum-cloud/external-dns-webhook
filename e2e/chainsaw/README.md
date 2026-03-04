# Chainsaw E2E Tests

This directory contains declarative end-to-end tests for the datum-dns-webhook project using [Chainsaw](https://kyverno.github.io/chainsaw/).

## Overview

Chainsaw replaces the previous bash-based validation script with declarative YAML tests that:
- Use Kubernetes API assertions directly (no port-forwarding needed)
- Provide clear, readable test definitions
- Generate machine-parseable reports (JUnit format)
- Offer better debugging and failure diagnostics

## Test Structure

Tests are organized in a folder-per-test structure:

```
chainsaw/
├── chainsaw-config.yaml          # Global configuration (timeouts, reporting)
├── 01-webhook-ready/             # Test 1: Webhook pod readiness
│   └── chainsaw-test.yaml
├── 02-external-dns-ready/        # Test 2: ExternalDNS pod readiness
│   └── chainsaw-test.yaml
├── 03-dnszone-exists/            # Test 3: DNSZone resource validation
│   └── chainsaw-test.yaml
├── 04-httproute-exists/          # Test 4: HTTPRoute resource validation
│   └── chainsaw-test.yaml
└── 05-dnsrecordset-created/      # Test 5: DNSRecordSet creation and content
    └── chainsaw-test.yaml
```

## Test Cases

### 01-webhook-ready
Verifies the datum-dns-webhook pod is ready and the deployment is available.
- Asserts pod has `Ready` condition status `True`
- Asserts deployment has `availableReplicas: 1`

### 02-external-dns-ready
Verifies the external-dns pod is ready and the deployment is available.
- Asserts pod has `Ready` condition status `True`
- Asserts deployment has `availableReplicas: 1`

### 03-dnszone-exists
Verifies the DNSZone `example-com` exists with correct spec.
- Asserts `spec.domainName: example.com`
- Asserts `spec.dnsZoneClassName: cloudflare-test`

### 04-httproute-exists
Verifies the HTTPRoute `whoami` exists with correct configuration.
- Asserts `hostnames` contains `whoami.example.com`
- Asserts annotation `external-dns.alpha.kubernetes.io/hostname` is set

### 05-dnsrecordset-created
Verifies ExternalDNS and the webhook created DNSRecordSet resources correctly.
- Asserts DNSRecordSet exists with label `external-dns.io/owner: external-dns-e2e`
- Asserts A record has correct `dnsZoneRef.name: example-com`
- Asserts `recordType: A`
- Asserts `records[0].name: whoami`
- Asserts ownership labels are present

## Running Tests

Tests are integrated into the E2E Taskfile:

```bash
# Run chainsaw tests directly
task test:chainsaw

# Run full E2E suite (setup + build + deploy + test)
task all

# Run tests as part of dev workflow
task test
```

## Prerequisites

Install Chainsaw:

```bash
# macOS
brew install kyverno/chainsaw/chainsaw

# Go install
go install github.com/kyverno/chainsaw@latest

# Direct download
# See: https://kyverno.github.io/chainsaw/main/install/
```

## Test Execution Flow

1. Tests run sequentially in numeric order (01, 02, 03, 04, 05)
2. Each test asserts resources exist with expected state
3. Tests do NOT create or delete resources (resources are managed by Taskfile)
4. Chainsaw uses the kubeconfig from `KUBECONFIG` environment variable
5. Assertions retry with timeout (default: 2 minutes per assertion)

## Why No Port-Forwarding?

Previous bash tests used port-forwarding to check `/readyz` endpoints and scrape metrics. This added complexity:
- Background process management (spawn/kill)
- Manual timeout handling
- Fragile cleanup on failure

With Chainsaw:
- **Readiness**: Kubernetes readiness probes already validate HTTP endpoints. If a pod is `Ready`, the endpoint works.
- **Metrics**: Metrics are internal observability, not functional correctness tests. The core test is: "Does the webhook create correct DNSRecordSets from HTTPRoutes?"

All validations use Kubernetes API assertions directly.

## Debugging Failed Tests

When a test fails, Chainsaw provides:
- Exact assertion that failed
- Expected vs actual resource state
- Full resource diff

Example failure output:

```
| 12:34:56 | dnsrecordset-created | @check-dnsrecordset-a-record | ASSERT    | RUN   | dns.networking.miloapis.com/v1alpha1/DNSRecordSet @ default/*
| 12:34:58 | dnsrecordset-created | @check-dnsrecordset-a-record | ASSERT    | ERROR | dns.networking.miloapis.com/v1alpha1/DNSRecordSet @ default/*
    Expected:
      spec.recordType: A
    Actual:
      spec.recordType: CNAME
```

You can also inspect resources manually:

```bash
# List all DNSRecordSets
kubectl get dnsrecordsets -n default -o yaml

# Check webhook logs
kubectl logs -n external-dns -l app=datum-dns-webhook

# Check ExternalDNS logs
kubectl logs -n external-dns -l app=external-dns
```

## Adding New Tests

To add a new test case:

1. Create a new directory: `chainsaw/06-my-new-test/`
2. Create `chainsaw-test.yaml` in that directory
3. Define your test using Chainsaw syntax

Example test structure:

```yaml
apiVersion: chainsaw.kyverno.io/v1alpha1
kind: Test
metadata:
  name: my-new-test
spec:
  description: What this test validates
  steps:
  - name: step-name
    try:
    - assert:
        resource:
          apiVersion: v1
          kind: Pod
          metadata:
            namespace: default
            labels:
              app: my-app
          status:
            (conditions[?type == 'Ready']):
            - status: "True"
```

## Configuration

Global test configuration in `chainsaw-config.yaml`:

- `timeouts.assert: 2m` - How long to retry assertions before failing
- `skipDelete: true` - Don't cleanup resources (Taskfile manages lifecycle)
- `reportFormat: JSON` - Output format for CI integration

## Migration from Bash

The previous bash script (`scripts/e2e-validation.sh`) is now **deprecated** and can be deleted after validating Chainsaw tests work.

Bash tests → Chainsaw equivalent:

| Bash Test | Chainsaw Test | Changes |
|-----------|---------------|---------|
| Test 1: Webhook readiness | 01-webhook-ready | No port-forward, use pod Ready condition |
| Test 2: ExternalDNS running | 02-external-dns-ready | No port-forward, use pod Ready condition |
| Test 3: DNSZone exists | 03-dnszone-exists | Direct assertion on resource |
| Test 4: Webhook metrics | **REMOVED** | Metrics are internal observability |
| Test 5: Test app ready | **Implicit in 04** | HTTPRoute implies app is ready |
| Test 6: HTTPRoute exists | 04-httproute-exists | Direct assertion on resource |
| Test 7-9: DNSRecordSet validation | 05-dnsrecordset-created | Combined into single test with steps |
| Test 10: Webhook metrics | **REMOVED** | Metrics are internal observability |

## References

- [Chainsaw Documentation](https://kyverno.github.io/chainsaw/)
- [JMESPath Expressions](https://jmespath.org/) - Used for assertions
- [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/) - HTTPRoute spec
