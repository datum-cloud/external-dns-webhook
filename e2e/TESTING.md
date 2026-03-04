# E2E Testing Quick Reference

## Running Tests

### Full E2E Suite (Recommended)

From repository root:

```bash
task dev:setup
task test:e2e
```

This runs:
1. `task dev:setup` - Creates KIND cluster with infrastructure, builds and deploys all components
2. `task test:e2e` - Runs Chainsaw E2E tests

### Run Tests Only

If cluster is already set up:

```bash
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig
task test:e2e
```

### Run Chainsaw Directly

```bash
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig
chainsaw test --test-dir e2e/chainsaw --config e2e/chainsaw/chainsaw-config.yaml
```

## Test Cases

| Test | Description | Validates |
|------|-------------|-----------|
| 01-httproute-creates-record | Full lifecycle test | Create HTTPRoute → DNSRecordSet appears → Delete HTTPRoute → DNSRecordSet removed |
| 02-txt-ownership-record | Ownership tracking | TXT record created for ExternalDNS ownership alongside A record |

## Expected Behavior

### Success Output

```
Running Chainsaw E2E tests...
Loading tests...
- 01-httproute-creates-record (.)
- 02-txt-ownership-record (.)

Running tests...
✓ 01-httproute-creates-record (65s)
✓ 02-txt-ownership-record (65s)

Tests summary:
- Passed: 2
- Failed: 0
- Skipped: 0
```

### Failure Example

If a test fails, Chainsaw shows:

```
✗ 01-httproute-creates-record (30s)
  Step: check-dnsrecordset-a-record
  Error: resource not found: DNSRecordSet
    Expected: metadata.labels["external-dns.io/owner"] = "external-dns-e2e"
    Actual: (no resources found)
```

## Debugging Failed Tests

### Check Webhook Logs

```bash
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig
task e2e:logs
```

Or directly:

```bash
kubectl logs -n external-dns -l app=datum-dns-webhook
```

### Check ExternalDNS Logs

```bash
kubectl logs -n external-dns -l app=external-dns
```

### Inspect Resources

```bash
# List all DNSRecordSets
kubectl get dnsrecordsets -A -o wide

# Describe DNSRecordSet
kubectl describe dnsrecordset -n default

# Check HTTPRoute
kubectl get httproute whoami -n default -o yaml

# Check DNSZone
kubectl get dnszone example-com -n default -o yaml
```

### Collect Full Diagnostics

```bash
task e2e:diag
```

This creates `e2e/e2e-diagnostics/` with:
- All resource manifests
- All pod logs
- Webhook metrics
- Timestamp of collection

## Common Issues

### Chainsaw not found

```
chainsaw: command not found
```

**Solution**: Install Chainsaw

```bash
# macOS
brew install kyverno/chainsaw/chainsaw

# Go install
go install github.com/kyverno/chainsaw@latest
```

### Test timeout

```
Error: assertion timeout exceeded (2m)
```

**Solution**: Check if pods are actually ready

```bash
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig
kubectl get pods -A
kubectl describe pod -n external-dns -l app=datum-dns-webhook
kubectl describe pod -n external-dns -l app=external-dns
```

Common causes:
- Image pull failures
- Deployment not ready
- Resource constraints in KIND

### DNSRecordSet not created

**Possible causes**:

1. **Webhook not discovering zones**:
   ```bash
   kubectl logs -n external-dns -l app=datum-dns-webhook | grep -i zone
   ```

2. **ExternalDNS not watching HTTPRoutes**:
   ```bash
   kubectl logs -n external-dns -l app=external-dns | grep -i httproute
   ```

3. **HTTPRoute not annotated correctly**:
   ```bash
   kubectl get httproute whoami -n default -o yaml | grep annotations -A 5
   ```

## Interactive Development Workflow

```bash
# 1. Setup environment (once)
task dev:setup

# 2. Export KUBECONFIG
export KUBECONFIG=~/.kube/datum-dns-webhook-e2e.kubeconfig

# 3. Make changes to webhook code

# 4. Rebuild and reload
docker build -t datum-dns-webhook:e2e-test .
kind load docker-image datum-dns-webhook:e2e-test --name datum-dns-webhook-e2e

# 5. Restart webhook
kubectl rollout restart -n external-dns deployment/datum-dns-webhook
kubectl rollout status -n external-dns deployment/datum-dns-webhook

# 6. Re-run tests
task test:e2e

# 7. View logs
task e2e:logs

# 8. When done
task e2e:cleanup
```

## CI Integration

GitHub Actions workflow (`.github/workflows/e2e.yaml`) runs:

```yaml
- name: Run E2E tests
  run: task ci:e2e
```

The `ci:e2e` task orchestrates:
- Cluster creation with base infrastructure
- Image build and deployment
- Test execution
- Automatic diagnostics collection on failure

Chainsaw outputs JUnit XML for test result visualization in GitHub Actions UI.

## Adding New Tests

Create new test folder:

```bash
mkdir -p e2e/chainsaw/03-my-new-test
```

Create `chainsaw-test.yaml`:

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

Run to verify:

```bash
task test:e2e
```

## References

- [Chainsaw Documentation](https://kyverno.github.io/chainsaw/)
- [JMESPath Tutorial](https://jmespath.org/tutorial.html)
- [Kubernetes Gateway API](https://gateway-api.sigs.k8s.io/)
- [ExternalDNS Documentation](https://kubernetes-sigs.github.io/external-dns/)
