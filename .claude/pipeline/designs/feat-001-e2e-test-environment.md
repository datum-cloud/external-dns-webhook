---
id: feat-001
title: E2E Test Environment Consolidation with test-infra
status: draft
created: 2026-02-23
author: architect
handoff_to: engineer
---

# E2E Test Environment Consolidation with test-infra

## Overview

This design consolidates the E2E testing infrastructure for datum-dns-webhook by adopting test-infra's remote Taskfile pattern. The goal is to provide streamlined developer workflows (`task dev:setup`, `task test:e2e`) and simplified CI orchestration (<50 lines of YAML) while maintaining full compatibility with existing Chainsaw tests.

**Scope**: Phase 1 (Consolidation) only. Phase 2 (Real DNS with Knot DNS) is deferred to a future feature.

## Requirements

### Functional Requirements
- FR1: Single-command cluster setup from repository root (`task dev:setup`)
- FR2: Single-command test execution from repository root (`task test:e2e`)
- FR3: CI workflow simplified to <50 lines with single orchestration command
- FR4: Existing Chainsaw tests continue to work without modification
- FR5: Component-specific manifests remain in this repository
- FR6: Diagnostic collection automated on failure

### Non-Functional Requirements
- NFR1: Local setup completes in under 10 minutes
- NFR2: CI execution completes in under 15 minutes
- NFR3: Setup is idempotent (safe to run multiple times)
- NFR4: Clear error messages for common failure scenarios
- NFR5: Documentation readable in under 5 minutes

## Design

### Architecture Overview

The design follows a layered approach where test-infra provides cluster lifecycle and base infrastructure, while this repository provides component-specific deployment and testing:

```
┌─────────────────────────────────────────────────────────────┐
│ Repository: datum-dns-webhook                               │
│                                                               │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ Root Taskfile.yml                                       │ │
│ │ - dev:setup    (orchestrator)                           │ │
│ │ - test:e2e     (orchestrator)                           │ │
│ │ - ci:e2e       (orchestrator)                           │ │
│ │ - e2e:*        (component-specific tasks)               │ │
│ └─────────────────┬───────────────────────────────────────┘ │
│                   │                                           │
│                   │ includes (remote)                         │
│                   ▼                                           │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ test-infra Taskfile                                     │ │
│ │ - kind:create                                           │ │
│ │ - flux:install                                          │ │
│ │ - infra:deploy (cert-manager, envoy-gateway)            │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                               │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ config/                                                 │ │
│ │ - base/ (webhook deployment)                            │ │
│ │ - dependencies/external-dns/ (Flux HelmRelease)         │ │
│ │ - overlays/test/ (test environment overlay)             │ │
│ │   - dns-operator/ (mock)                                │ │
│ │   - dns-fixtures/ (DNSZone, DNSZoneClass)               │ │
│ │   - test-app/ (whoami + HTTPRoute)                      │ │
│ └─────────────────────────────────────────────────────────┘ │
│                                                               │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ e2e/chainsaw/                                           │ │
│ │ - 01-httproute-creates-record/                          │ │
│ │ - 02-txt-ownership-record/                              │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### Task Execution Flow

#### Developer Setup Flow (`task dev:setup`)

```
task dev:setup
├─> test-infra:kind:create
│   └─> Creates KIND cluster with name datum-dns-webhook-e2e
│       Exports KUBECONFIG to ~/.kube/datum-dns-webhook-e2e.kubeconfig
├─> test-infra:flux:install
│   └─> Installs Flux components to flux-system namespace
├─> test-infra:infra:deploy
│   ├─> cert-manager (namespace: cert-manager)
│   └─> envoy-gateway (namespace: envoy-gateway-system)
├─> e2e:build
│   ├─> docker build -t datum-dns-webhook:e2e-test
│   └─> kind load docker-image datum-dns-webhook:e2e-test
└─> e2e:deploy
    ├─> kubectl apply -k config/overlays/test/dns-operator/
    ├─> kubectl apply -k config/overlays/test/dns-fixtures/
    ├─> kubectl apply -k config/dependencies/external-dns/ (Flux HelmRelease)
    ├─> flux reconcile helmrelease external-dns -n external-dns
    ├─> kubectl apply -k config/overlays/test/ (webhook + test-app)
    └─> kubectl wait for all deployments

Total: ~8 minutes local
```

#### Test Execution Flow (`task test:e2e`)

```
task test:e2e
└─> chainsaw test --test-dir e2e/chainsaw --config e2e/chainsaw/chainsaw-config.yaml
    ├─> 01-httproute-creates-record (lifecycle test)
    └─> 02-txt-ownership-record (ownership tracking)

Total: ~2 minutes
```

#### CI Flow (`task ci:e2e`)

```
task ci:e2e
├─> test-infra:kind:create (with CI-optimized settings)
├─> test-infra:flux:install
├─> test-infra:infra:deploy
├─> e2e:build
├─> e2e:deploy
├─> e2e:test:chainsaw (with JUnit output)
└─> [on failure] e2e:diagnostics

Total: ~12-15 minutes CI
```

### Root Taskfile.yml Design

The root Taskfile will be created/enhanced with the following structure:

```yaml
version: '3'

includes:
  # Remote include from test-infra
  # Can be overridden with TASKFILE_TESTINFRA_PATH environment variable
  test-infra:
    taskfile: '{{default "https://raw.githubusercontent.com/datumforge/test-infra/main/Taskfile.yml" .TASKFILE_TESTINFRA_PATH}}'
    internal: true  # Hide test-infra tasks from `task --list`

vars:
  CLUSTER_NAME: datum-dns-webhook-e2e
  KUBECONFIG: "{{.HOME}}/.kube/{{.CLUSTER_NAME}}.kubeconfig"
  IMAGE_NAME: datum-dns-webhook:e2e-test

tasks:
  # High-level orchestration tasks
  dev:setup:
    desc: Create complete E2E environment for interactive development
    cmds:
      - task: test-infra:kind:create
        vars:
          CLUSTER_NAME: '{{.CLUSTER_NAME}}'
          KUBECONFIG: '{{.KUBECONFIG}}'
      - task: test-infra:flux:install
        vars:
          KUBECONFIG: '{{.KUBECONFIG}}'
      - task: test-infra:infra:deploy
        vars:
          KUBECONFIG: '{{.KUBECONFIG}}'
      - task: e2e:build
      - task: e2e:deploy
      - cmd: |
          echo ""
          echo "✓ E2E environment ready!"
          echo ""
          echo "Export KUBECONFIG:"
          echo "  export KUBECONFIG={{.KUBECONFIG}}"
          echo ""
          echo "Useful commands:"
          echo "  task test:e2e      # Run E2E tests"
          echo "  task e2e:logs      # View component logs"
          echo "  task e2e:diag      # Collect diagnostics"
          echo "  task e2e:cleanup   # Destroy environment"
        silent: true

  test:e2e:
    desc: Run E2E tests (requires environment from dev:setup)
    cmds:
      - task: e2e:test:chainsaw

  ci:e2e:
    desc: Run full E2E suite in CI (setup + test + cleanup on failure)
    cmds:
      - task: dev:setup
      - task: test:e2e
      - defer: {task: e2e:diag:on-failure}

  # Component-specific tasks (e2e namespace)
  e2e:build:
    desc: Build and load webhook image into KIND
    dir: .
    cmds:
      - docker build -t {{.IMAGE_NAME}} .
      - kind load docker-image {{.IMAGE_NAME}} --name {{.CLUSTER_NAME}}

  e2e:deploy:
    desc: Deploy DNS operator, fixtures, webhook, and test app
    vars:
      KUBECONFIG: '{{.KUBECONFIG}}'
    cmds:
      - cmd: echo "Deploying DNS operator (mock)..."
        silent: true
      - kubectl apply -k config/overlays/test/dns-operator/
      - kubectl wait --for=condition=available --timeout=300s -n dns-operator-system deployment/mock-dns-operator

      - cmd: echo "Deploying DNS fixtures..."
        silent: true
      - kubectl apply -k config/overlays/test/dns-fixtures/

      - cmd: echo "Deploying ExternalDNS via Flux HelmRelease..."
        silent: true
      - kubectl apply -k config/dependencies/external-dns/
      - flux reconcile helmrelease external-dns -n external-dns --timeout=5m
      - kubectl wait --for=condition=available --timeout=300s -n external-dns deployment/external-dns

      - cmd: echo "Deploying webhook + test application..."
        silent: true
      - kubectl apply -k config/overlays/test/
      - kubectl wait --for=condition=available --timeout=300s -n external-dns deployment/datum-dns-webhook
      - kubectl wait --for=condition=available --timeout=300s -n default deployment/whoami

  e2e:test:chainsaw:
    desc: Run Chainsaw E2E tests
    vars:
      KUBECONFIG: '{{.KUBECONFIG}}'
    dir: e2e
    cmds:
      - chainsaw test --test-dir chainsaw --config chainsaw/chainsaw-config.yaml

  e2e:logs:
    desc: Show logs from key components
    vars:
      KUBECONFIG: '{{.KUBECONFIG}}'
    cmds:
      - cmd: echo "=== Webhook Logs ===" && echo ""
        silent: true
      - kubectl logs -n external-dns -l app=datum-dns-webhook --tail=50
      - cmd: echo "" && echo "=== ExternalDNS Logs ===" && echo ""
        silent: true
      - kubectl logs -n external-dns -l app=external-dns --tail=50

  e2e:diag:
    desc: Collect full diagnostics
    vars:
      KUBECONFIG: '{{.KUBECONFIG}}'
    dir: e2e
    cmds:
      - bash scripts/diagnostics.sh

  e2e:diag:on-failure:
    desc: Collect diagnostics only if previous task failed
    vars:
      KUBECONFIG: '{{.KUBECONFIG}}'
    dir: e2e
    cmds:
      - cmd: |
          if [ "{{.TASK_EXIT_CODE}}" != "0" ]; then
            echo "Test failed, collecting diagnostics..."
            bash scripts/diagnostics.sh
          fi
    ignore_error: true

  e2e:cleanup:
    desc: Delete KIND cluster and kubeconfig
    cmds:
      - kind delete cluster --name {{.CLUSTER_NAME}}
      - rm -f {{.KUBECONFIG}}

  # Existing build/test tasks (preserved)
  build:
    desc: Build datum-dns-webhook binary
    cmds:
      - mkdir -p bin
      - go build -o bin/datum-dns-webhook .
    sources:
      - '**/*.go'
      - go.mod
      - go.sum
    generates:
      - bin/datum-dns-webhook

  test:
    desc: Run unit tests with race detection
    cmds:
      - go test -v -race ./...

  lint:
    desc: Run golangci-lint
    cmds:
      - golangci-lint run

  docker:
    desc: Build Docker image
    cmds:
      - docker build -t datum-dns-webhook:latest .

  clean:
    desc: Clean build artifacts
    cmds:
      - rm -rf bin/
```

### CI Workflow Design

The GitHub Actions workflow will be simplified to:

```yaml
name: E2E Tests

on:
  pull_request:
    branches: [main]
  push:
    branches: [main]
  workflow_dispatch:

env:
  GO_VERSION: '1.22'

jobs:
  e2e:
    name: E2E Tests
    runs-on: ubuntu-latest
    timeout-minutes: 20

    steps:
    - name: Checkout repository
      uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v5
      with:
        go-version: ${{ env.GO_VERSION }}

    - name: Install Task
      run: |
        sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

    - name: Install Chainsaw
      run: |
        go install github.com/kyverno/chainsaw@latest

    - name: Run E2E tests
      run: task ci:e2e

    - name: Upload diagnostics
      if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: e2e-diagnostics
        path: e2e/e2e-diagnostics/
        retention-days: 7
```

**Line count**: ~40 lines (vs. current 118 lines)

### e2e/Taskfile.yml Disposition

**Decision**: Remove `e2e/Taskfile.yml` entirely.

**Rationale**:
- All tasks are now accessible from repository root via `e2e:*` namespace
- Eliminates confusion about which Taskfile to use
- Single source of truth for task definitions
- Simpler mental model for developers

**Migration**: All functionality from `e2e/Taskfile.yml` will be moved to the root Taskfile under the `e2e:*` namespace.

### Remote Taskfile Include Strategy

**Pattern**: Use test-infra remote include with environment variable override

```yaml
includes:
  test-infra:
    taskfile: '{{default "https://raw.githubusercontent.com/datumforge/test-infra/main/Taskfile.yml" .TASKFILE_TESTINFRA_PATH}}'
    internal: true
```

**Benefits**:
1. **Zero local dependency**: Works out of the box for new contributors
2. **Local development**: Set `TASKFILE_TESTINFRA_PATH=../test-infra/Taskfile.yml` to use local clone
3. **Version pinning**: Can pin to specific commit SHA in production
4. **Automatic updates**: Pick up test-infra improvements automatically

**Risk mitigation**:
- Pin to commit SHA for critical CI workflows
- Weekly CI job tests against test-infra@main to catch breaking changes early

### File Modifications Required

| File | Action | Description |
|------|--------|-------------|
| `Taskfile.yml` | Modify | Replace existing with new design |
| `e2e/Taskfile.yml` | Delete | Consolidated into root Taskfile |
| `.github/workflows/e2e.yaml` | Modify | Simplify to <50 lines |
| `e2e/README.md` | Modify | Update for new workflow |
| `e2e/TESTING.md` | Modify | Update quick reference |
| `README.md` | Modify | Add E2E testing section |
| `e2e/scripts/diagnostics.sh` | Keep | No changes (already correct) |
| `e2e/manifests/**/*` | Keep | No changes (component-specific) |
| `e2e/chainsaw/**/*` | Keep | No changes (tests unchanged) |

### Migration Path from Current Setup

**Phase 1: Preparation** (Non-breaking)
1. Create new root `Taskfile.yml` with `e2e:*` tasks
2. Keep existing `e2e/Taskfile.yml` for compatibility
3. Update documentation to reference root tasks

**Phase 2: Cutover** (Breaking)
1. Update CI workflow to use root tasks
2. Verify CI passes
3. Delete `e2e/Taskfile.yml`
4. Update remaining documentation

**Phase 3: Validation**
1. Manual testing of all workflows
2. Verify CI stability over 3-5 runs
3. Close feature as complete

**Rollback plan**: If issues arise, revert to commit before Phase 2 cutover.

### Component Integration Points

#### test-infra Dependencies

The webhook assumes test-infra provides:

| Component | Namespace | Verification Method |
|-----------|-----------|---------------------|
| KIND cluster | - | `kubectl cluster-info` |
| Flux CD | `flux-system` | `flux check` |
| cert-manager | `cert-manager` | Pod ready check |
| Envoy Gateway | `envoy-gateway-system` | Pod ready check |
| Gateway API CRDs | - | `kubectl get crd gateways.gateway.networking.k8s.io` |

**Contract**: test-infra guarantees these components are ready before returning from `infra:deploy`.

#### Component Deployment Order

Critical ordering constraints:

```
1. DNS Operator CRDs          # Must exist before DNSZone/DNSRecordSet
   └─> DNS Operator (mock)    # Must be ready to mark zones as ready
2. DNSZoneClass               # Referenced by DNSZone
   └─> DNSZone                # Discovered by webhook
3. Webhook deployment         # Must discover zones
   └─> ExternalDNS            # Consumes webhook API
4. Test application           # Creates HTTPRoute
   └─> Chainsaw tests         # Validates end-to-end flow
```

**Implementation**: Sequential `kubectl apply` with `kubectl wait` between stages.

### Testing Approach

#### Existing Tests (No Changes)

The current Chainsaw tests remain unchanged:
- `01-httproute-creates-record`: Full lifecycle (create → verify → delete → verify)
- `02-txt-ownership-record`: Ownership tracking validation

**Guarantee**: No test assertions will be modified in this feature.

#### Integration Testing

**Test ID**: IT-001
**Name**: Root task orchestration
**Verify**: `task dev:setup` completes successfully from clean state

**Test ID**: IT-002
**Name**: Test execution
**Verify**: `task test:e2e` passes all Chainsaw tests

**Test ID**: IT-003
**Name**: CI workflow
**Verify**: CI workflow completes in <15 minutes

**Test ID**: IT-004
**Name**: Diagnostics collection
**Verify**: `task e2e:diag` creates `e2e-diagnostics/` with expected files

**Test ID**: IT-005
**Name**: Idempotency
**Verify**: Running `task dev:setup` twice succeeds (second run detects existing cluster)

#### Manual Validation Checklist

- [ ] Fresh clone of repo → `task dev:setup` → succeeds
- [ ] `task test:e2e` → all tests pass
- [ ] `task e2e:logs` → displays webhook and external-dns logs
- [ ] `task e2e:diag` → creates diagnostics directory with all expected files
- [ ] `task e2e:cleanup` → cluster deleted, kubeconfig removed
- [ ] CI workflow passes on PR
- [ ] CI diagnostics uploaded on failure
- [ ] Documentation accurate (verified by following it step-by-step)

### Error Handling and Diagnostics

#### Common Failure Scenarios

**Scenario 1**: test-infra repository not accessible
```
Error: Failed to fetch remote Taskfile
Solution: Set TASKFILE_TESTINFRA_PATH=../test-infra/Taskfile.yml
```

**Scenario 2**: Docker not running
```
Error: Cannot connect to Docker daemon
Solution: Start Docker Desktop / dockerd
```

**Scenario 3**: Insufficient Docker resources
```
Error: Pod stuck in Pending state
Solution: Increase Docker memory to 4GB minimum
```

**Scenario 4**: Port conflicts
```
Error: KIND cluster creation failed (port already in use)
Solution: Stop existing cluster or change CLUSTER_NAME
```

#### Diagnostic Collection Strategy

**Automatic triggers**:
1. CI test failure → diagnostics collected and uploaded
2. Manual trigger → `task e2e:diag` anytime

**Collected artifacts** (via `e2e/scripts/diagnostics.sh`):
- Cluster info dump
- All pod status
- Webhook logs (last 500 lines)
- ExternalDNS logs (last 500 lines)
- DNS resources (DNSZone, DNSRecordSet)
- Gateway API resources (HTTPRoute)
- Events (sorted by timestamp)
- Webhook metrics (if accessible)

**Output location**: `e2e/e2e-diagnostics/` (gitignored)

### Security Considerations

**None**: This feature is purely infrastructure/tooling changes with no security impact.

The webhook's RBAC permissions and authorization model remain unchanged.

### Performance Considerations

#### Time Budgets

| Phase | Target (local) | Target (CI) | Notes |
|-------|----------------|-------------|-------|
| KIND cluster | 1 min | 2 min | Depends on Docker/machine |
| Flux install | 1 min | 1 min | Predictable |
| Infrastructure | 2 min | 3 min | cert-manager + envoy-gateway |
| Image build | 1 min | 2 min | Go build + Docker |
| Deploy | 2 min | 3 min | All components |
| Tests | 2 min | 3 min | Chainsaw execution |
| **Total** | **9 min** | **14 min** | Within budget |

**Optimization opportunities** (future):
- Pre-built infrastructure images (save ~2 min)
- Parallel deployment where safe (save ~1 min)
- Cached Go builds (save ~30 sec)

#### Resource Requirements

**Local development**:
- Docker: 4GB RAM minimum, 8GB recommended
- Disk: 10GB free space
- CPU: 2 cores minimum, 4 recommended

**CI (GitHub Actions)**:
- `runs-on: ubuntu-latest` (7GB RAM, 2 cores)
- Sufficient for current requirements

## Implementation Plan

### Step 1: Create Root Taskfile with test-infra Integration
**Estimated effort**: 2 hours

**Tasks**:
1. Create new `Taskfile.yml` at repository root with test-infra remote include
2. Define `dev:setup`, `test:e2e`, `ci:e2e` orchestration tasks
3. Define `e2e:*` namespace tasks (build, deploy, test, logs, diag, cleanup)
4. Preserve existing build/test/lint/docker tasks
5. Test locally: `task --list` shows expected tasks

**Acceptance criteria**:
- [ ] `task dev:setup` creates complete environment
- [ ] `task test:e2e` runs Chainsaw tests
- [ ] `task e2e:logs` displays component logs
- [ ] `task e2e:cleanup` destroys environment
- [ ] Existing `e2e/Taskfile.yml` still works (not deleted yet)

### Step 2: Simplify CI Workflow
**Estimated effort**: 1 hour

**Tasks**:
1. Create new `.github/workflows/e2e.yaml` (<50 lines)
2. Update to call `task ci:e2e` instead of manual steps
3. Verify prerequisite installation steps are correct
4. Test on feature branch, verify diagnostics upload works

**Acceptance criteria**:
- [ ] Workflow YAML is <50 lines
- [ ] Workflow calls `task ci:e2e`
- [ ] Diagnostics automatically uploaded on failure
- [ ] Workflow passes on feature branch

### Step 3: Update Documentation
**Estimated effort**: 1 hour

**Tasks**:
1. Update `e2e/README.md` with new workflow instructions
2. Update `e2e/TESTING.md` quick reference
3. Add E2E testing section to root `README.md`
4. Mark old `e2e/Taskfile.yml` instructions as deprecated

**Acceptance criteria**:
- [ ] Documentation reflects new workflow
- [ ] Prerequisites section updated (add Task, Chainsaw)
- [ ] Examples use root tasks (`task dev:setup` not `cd e2e && task all`)
- [ ] Troubleshooting section remains accurate

### Step 4: Remove Old Taskfile
**Estimated effort**: 30 minutes

**Tasks**:
1. Delete `e2e/Taskfile.yml`
2. Remove any remaining references to `cd e2e && task`
3. Final documentation pass
4. Verify CI still passes

**Acceptance criteria**:
- [ ] `e2e/Taskfile.yml` deleted
- [ ] All documentation uses root tasks
- [ ] CI workflow passes
- [ ] No broken references to old workflow

### Step 5: End-to-End Validation
**Estimated effort**: 1 hour

**Tasks**:
1. Fresh clone of repo
2. Follow documentation step-by-step
3. Verify all workflows work as documented
4. Run CI multiple times to verify stability
5. Test failure scenarios (e.g., kill webhook pod mid-test)

**Acceptance criteria**:
- [ ] All manual validation checklist items pass
- [ ] Documentation is accurate when followed literally
- [ ] CI stable across 3+ runs
- [ ] Failure scenarios produce helpful diagnostics

**Total estimated effort**: 5.5 hours (~1 day)

## Handoff

<!-- This section is read by implementation agents -->

### Decisions Made

**Decision 1: Single root Taskfile, remove e2e/Taskfile.yml**
**Rationale**: Eliminates confusion about which Taskfile to use. All tasks accessible from repository root with `e2e:*` namespace. Single source of truth.

**Decision 2: Remote include with environment variable override**
**Rationale**: Zero setup friction for new contributors (URL works immediately), while allowing local development via `TASKFILE_TESTINFRA_PATH`. Best of both worlds.

**Decision 3: CI workflow calls single `task ci:e2e` command**
**Rationale**: Moves orchestration logic from YAML to Taskfile where it's easier to test and iterate. CI becomes thin wrapper.

**Decision 4: Keep existing manifests and tests unchanged**
**Rationale**: This is purely infrastructure consolidation. Zero risk to test coverage or component behavior.

**Decision 5: Sequential deployment with kubectl wait**
**Rationale**: Ensures dependencies are ready before deploying dependents. Explicit ordering is clearer than parallel with complex wait logic.

**Decision 6: Config directory structure following kustomize best practices**
**Rationale**: Use `config/base` for webhook deployment, `config/dependencies/external-dns` for Flux HelmRelease, and `config/overlays/test` for test environment. This follows standard kustomize patterns and separates concerns clearly.

**Decision 7: Flux HelmRelease for external-dns**
**Rationale**: Flux is already installed in the cluster. Using HelmRelease mimics real production environments and provides GitOps-native deployment patterns.

### Open Questions

**Q1: Should we pin test-infra to specific commit SHA in CI?**
**Status**: Non-blocking
**Recommendation**: Start with `@main`, add pinning if instability occurs. Weekly CI job can test `@main` proactively.

**Q2: Should we add Makefile targets that call Task for backward compatibility?**
**Status**: Non-blocking
**Recommendation**: Not needed. Makefile is already deprecated (deleted in recent commits). Task is the standard.

### Implementation Notes

**For engineer**:

1. **Test locally first**: Before updating CI, verify the root Taskfile works end-to-end on your machine. This catches 90% of issues.

2. **Preserve existing behavior**: The current `e2e/Taskfile.yml` tasks work correctly. Your goal is to replicate that behavior in the root Taskfile, not improve it (yet).

3. **CI debugging**: If CI fails, the diagnostics artifact will contain all logs. Download it from the Actions UI to debug locally.

4. **test-infra dependency**: If test-infra is not accessible, set `TASKFILE_TESTINFRA_PATH=../test-infra/Taskfile.yml` and clone test-infra next to datum-dns-webhook.

5. **Idempotency**: The `kind:create` task should detect existing clusters. Test by running `task dev:setup` twice.

6. **No test changes**: Do not modify any files in `e2e/chainsaw/`. If tests fail, the issue is in deployment, not the tests.

7. **Documentation accuracy**: After writing docs, follow them literally (in a fresh terminal) to verify they're correct.

### Integration Points

**test-infra tasks consumed**:
- `kind:create` (creates cluster, sets KUBECONFIG)
- `flux:install` (installs Flux components)
- `infra:deploy` (deploys cert-manager, envoy-gateway)

**test-infra contract**:
- These tasks are idempotent
- They accept `CLUSTER_NAME` and `KUBECONFIG` variables
- They wait for components to be ready before returning

**If test-infra API changes**: The remote include pattern means we'll automatically pick up changes. If breaking changes occur, pin to last known good commit SHA while test-infra is updated.

### Success Criteria

**Feature is complete when**:
1. Root `Taskfile.yml` provides all required tasks
2. CI workflow is <50 lines and calls `task ci:e2e`
3. Documentation updated and accurate
4. `e2e/Taskfile.yml` deleted
5. CI passes consistently (3+ runs)
6. Manual validation checklist complete

**Regression prevention**:
- Existing Chainsaw tests must pass without modification
- Deployment topology unchanged (same namespaces, same components)
- Time budgets met (10 min local, 15 min CI)

### Future Considerations

These are explicitly out of scope for this feature but worth noting:

1. **Phase 2 - Real DNS with Knot DNS**: Will be separate feature (feat-XXX)
2. **Parallel deployment optimization**: Could save 1-2 minutes
3. **Pre-built infrastructure images**: Could save 2 minutes
4. **Multiple webhook instances**: Test multi-tenant scenarios
5. **Upgrade testing**: Test webhook upgrades without downtime

---

**Status**: Ready for implementation
**Approved by**: (pending)
**Assigned to**: engineer
