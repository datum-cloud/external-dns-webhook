---
handoff:
  id: feat-001
  from: product-planner
  to: engineer
  created: 2026-02-23T18:00:00Z
  context_summary: |
    Consolidate E2E test infrastructure using test-infra's Taskfile pattern.
    Provides single-command dev:setup and test:e2e workflows.
    Phase 2 (real DNS with Knot DNS) deferred to separate feature.
  decisions_made:
    - decision: "Use Flux HelmRelease for external-dns deployment"
      rationale: "Flux is already installed in the cluster. Using HelmRelease mimics real production environments and provides GitOps-native deployment patterns."
    - decision: "Use config/ directory with kustomize best practices"
      rationale: "config/base for webhook, config/dependencies/external-dns for Flux HelmRelease, config/overlays/test for test fixtures. Follows standard kustomize patterns and separates concerns clearly."
    - decision: "Test execution budget: 10 minutes local, 15 minutes CI"
      rationale: "Matches existing Chainsaw timeout patterns."
    - decision: "Use test-infra remote Taskfile include pattern"
      rationale: "Eliminates duplicate infrastructure code while maintaining local control over component-specific tasks."
    - decision: "Phase 2 (real DNS with Knot DNS) deferred to separate feature"
      rationale: "User decision to tackle consolidation first. Real DNS integration will be spec'd separately."
  open_questions: []
  assumptions:
    - "test-infra Taskfile supports remote include via HTTP (standard Task feature)"
  platform_capabilities:
    quota: "Not applicable"
    insights: "Not applicable"
    telemetry: "Existing webhook metrics coverage retained"
    activity: "Not applicable"
---

# Product Specification: E2E Test Environment with test-infra

**Feature ID**: feat-001
**Spec Version**: 1.1
**Status**: Ready for Implementation
**Author**: product-planner
**Date**: 2026-02-23

## Executive Summary

This specification details the consolidation of E2E testing infrastructure for datum-dns-webhook using the test-infra remote Taskfile pattern. The goal is to provide a streamlined developer experience with single-command setup and test execution.

**Scope**: This specification covers Phase 1 (Consolidation) only. Phase 2 (Real DNS with Knot DNS) will be specified separately as a future feature.

## Design Decisions

### DD-001: Flux HelmRelease for external-dns

**Decision**: Use Flux HelmRelease for external-dns deployment.

**Alternatives Considered**:
1. Flux HelmRelease (chosen) - GitOps-native, mimics production environment
2. Kustomize - Simpler but doesn't reflect real deployment patterns
3. Raw manifests - Simplest but least maintainable

**Rationale**: Flux is already installed in the test cluster via test-infra. Using HelmRelease:
- Mimics real production environments
- Provides GitOps-native deployment patterns
- Enables Helm chart configuration via values
- Allows testing of the same deployment method used in production

### DD-002: Component overlay lives in this repository

**Decision**: Keep DNS fixtures, mock operator, and webhook deployment manifests in `datum-dns-webhook/e2e/manifests/`.

**Rationale**:
- Tight coupling between webhook and its test fixtures
- Allows independent iteration without test-infra PRs
- test-infra provides cluster lifecycle; this repo provides component deployment
- Clear separation of concerns: infrastructure vs. application

### DD-003: Test execution time budget

**Decision**: 10 minutes local, 15 minutes CI.

**Breakdown**:
| Phase | Component | Time Budget |
|-------|-----------|-------------|
| Setup | KIND cluster creation | 2 min |
| Setup | Infrastructure (Flux, cert-manager, Envoy Gateway) | 3 min |
| Build | Webhook image build and load | 1 min |
| Deploy | Mock operator + fixtures + external-dns | 2 min |
| Test | Chainsaw tests | 2 min |
| **Total** | | **10 min local / 15 min CI** |

---

## User Stories

### US-001: Developer Single-Command Setup

**As a** webhook developer  
**I want to** run a single command to create a complete E2E environment  
**So that** I can quickly validate my changes locally

**Acceptance Criteria**:
- [ ] `task dev:setup` from repository root provisions KIND cluster with all infrastructure
- [ ] Command completes in under 8 minutes on a standard development machine
- [ ] Command is idempotent (can be run multiple times safely)
- [ ] Clear output shows progress through setup stages
- [ ] On completion, prints `KUBECONFIG` export instruction and available commands
- [ ] Requires only Docker, Task, and Chainsaw as prerequisites

**Test Scenarios**:
1. Fresh setup: No cluster exists, command creates everything
2. Existing cluster: Command detects existing cluster and continues
3. Partial failure: If infrastructure deployment fails, error message indicates which component failed

---

### US-002: Developer Single-Command Test

**As a** webhook developer  
**I want to** run all E2E tests with a single command  
**So that** I can verify the webhook works correctly

**Acceptance Criteria**:
- [ ] `task test:e2e` from repository root runs all Chainsaw tests
- [ ] Tests complete in under 5 minutes (excluding setup)
- [ ] Test results display clear pass/fail status for each test case
- [ ] Failed tests show which assertion failed and expected vs. actual values
- [ ] JUnit XML report generated for CI integration

**Test Scenarios**:
1. All tests pass: Clean output showing 2/2 tests passed
2. Test failure: Clear indication of which test failed and why
3. Environment not ready: Helpful error message if cluster doesn't exist

---

### US-003: Simplified CI Workflow

**As a** CI system  
**I want to** run E2E tests with minimal orchestration  
**So that** failures are easier to diagnose

**Acceptance Criteria**:
- [ ] `.github/workflows/e2e.yaml` reduced to <50 lines of YAML
- [ ] CI workflow calls `task ci:e2e` which handles all orchestration
- [ ] Diagnostics automatically collected on failure
- [ ] Workflow completes in under 15 minutes
- [ ] Clear job status in GitHub Actions UI

**Current vs. Target**:

| Metric | Current | Target |
|--------|---------|--------|
| Workflow YAML lines | 118 | <50 |
| Orchestration steps | 16 | 5 |
| Manual waits | 5 | 0 (handled by tasks) |
| Failure diagnostics | Manual | Automatic |

---

## Task Breakdown

#### Task 1.1: Create root Taskfile.yml with test-infra integration

**Description**: Create a root-level `Taskfile.yml` that includes test-infra tasks remotely and provides high-level `dev:setup` and `test:e2e` targets.

**Technical Approach**:
```yaml
version: '3'

includes:
  # Remote include from test-infra (requires Task v3.35+)
  infra:
    taskfile: https://raw.githubusercontent.com/datum-cloud/test-infra/main/Taskfile.yml
    internal: true

vars:
  CLUSTER_NAME: datum-dns-webhook-e2e
  KUBECONFIG: "{{.HOME}}/.kube/{{.CLUSTER_NAME}}.kubeconfig"

tasks:
  dev:setup:
    desc: Create complete E2E environment
    cmds:
      - task: infra:kind:create
        vars: {CLUSTER_NAME: "{{.CLUSTER_NAME}}"}
      - task: infra:flux:install
      - task: infra:base:deploy
      - task: e2e:deploy

  test:e2e:
    desc: Run E2E tests
    cmds:
      - task: e2e:test:chainsaw
```

**Acceptance Criteria**:
- [ ] Root Taskfile.yml created at `/Users/scotwells/repos/datum-dns-webhook/Taskfile.yml`
- [ ] Includes test-infra tasks via remote URL or local path
- [ ] `task dev:setup` creates complete environment
- [ ] `task test:e2e` runs Chainsaw tests
- [ ] `task --list` shows all available tasks with descriptions

**Estimated Effort**: 2 hours

---

#### Task 1.2: Migrate e2e/Taskfile.yml tasks to root

**Description**: Move or consolidate tasks from `e2e/Taskfile.yml` to the root `Taskfile.yml`, eliminating the need to `cd e2e` for common operations.

**Technical Approach**:
- Create `e2e:` namespace for E2E-specific tasks
- Keep `e2e/Taskfile.yml` for Chainsaw-specific configuration
- Root Taskfile orchestrates high-level flows

**Acceptance Criteria**:
- [ ] All developer workflows accessible from repository root
- [ ] `task e2e:logs` shows component logs
- [ ] `task e2e:diagnostics` collects debug information
- [ ] `task e2e:cleanup` destroys environment
- [ ] `e2e/Taskfile.yml` either removed or reduced to Chainsaw-only tasks

**Estimated Effort**: 1 hour

---

#### Task 1.3: Simplify CI workflow

**Description**: Refactor `.github/workflows/e2e.yaml` to use the new task targets instead of manual orchestration.

**Technical Approach**:
```yaml
name: E2E Tests
on:
  pull_request:
    branches: [main]
  push:
    branches: [main]

jobs:
  e2e:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
    - uses: actions/checkout@v4

    - name: Install prerequisites
      run: |
        # Install Task, KIND, kubectl, Chainsaw
        sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

    - name: Run E2E tests
      run: task ci:e2e

    - name: Upload diagnostics
      if: failure()
      uses: actions/upload-artifact@v4
      with:
        name: e2e-diagnostics
        path: e2e-diagnostics/
```

**Acceptance Criteria**:
- [ ] Workflow YAML under 50 lines
- [ ] Single `task ci:e2e` command handles orchestration
- [ ] No explicit `kubectl wait` commands in workflow
- [ ] Diagnostics collected automatically on failure
- [ ] Workflow passes on current main branch

**Estimated Effort**: 2 hours

---

#### Task 1.4: Update documentation

**Description**: Update `e2e/README.md` and `e2e/TESTING.md` to reflect the new workflow.

**Acceptance Criteria**:
- [ ] README explains new `task dev:setup` / `task test:e2e` workflow
- [ ] Prerequisites section updated
- [ ] Old workflow documented as deprecated (if still supported)
- [ ] Troubleshooting section remains accurate
- [ ] Documentation readable in <5 minutes

**Estimated Effort**: 1 hour

---

## Technical Architecture

### Component Deployment Topology

```
KIND Cluster (datum-dns-webhook-e2e)
├── kube-system (KIND default)
├── flux-system
│   └── flux components
├── cert-manager
│   └── cert-manager pods
├── envoy-gateway-system
│   └── Envoy Gateway pods
├── dns-system
│   └── dns-operator (mock)
├── external-dns
│   ├── datum-dns-webhook
│   └── external-dns
└── default
    ├── whoami (test app)
    ├── DNSZone (example-com)
    └── DNSRecordSet (test-app-a)
```

### Task Execution Flow

```
task dev:setup
    ├── test-infra: kind:create
    ├── test-infra: flux:install
    ├── test-infra: base:deploy
    │   ├── cert-manager
    │   └── envoy-gateway
    └── e2e:deploy
        ├── dns-operator (mock)
        ├── dns-fixtures
        ├── external-dns + webhook
        └── test-app

task test:e2e
    └── chainsaw test --test-dir e2e/chainsaw
        ├── 01-httproute-creates-record
        └── 02-txt-ownership-record
```

### Remote Taskfile Include Pattern

The root `Taskfile.yml` will use Task's remote include feature:

```yaml
includes:
  test-infra:
    taskfile: https://raw.githubusercontent.com/datum-cloud/test-infra/main/Taskfile.yml
    # Or use local path during development:
    # taskfile: ../test-infra/Taskfile.yml
```

This allows:
- No duplication of cluster lifecycle code
- Automatic updates when test-infra changes (pinnable via ref)
- Local override during development

---

## Testing Strategy

### Unit Tests
Not applicable for this feature (infrastructure changes only).

### Integration Tests
- Existing Chainsaw tests serve as integration tests
- No changes to existing test assertions required for Phase 1

### E2E Tests

| Test ID | Name | Description |
|---------|------|-------------|
| 01 | httproute-creates-record | Existing: HTTPRoute lifecycle |
| 02 | txt-ownership-record | Existing: TXT ownership tracking |

### Manual Verification Checklist

- [ ] `task dev:setup` completes successfully from clean state
- [ ] `task test:e2e` passes all tests
- [ ] `task e2e:cleanup` removes cluster
- [ ] CI workflow passes on PR
- [ ] Documentation is accurate

---

## Documentation Requirements

### Files to Update

| File | Changes |
|------|---------|
| `e2e/README.md` | New workflow instructions, updated prerequisites |
| `e2e/TESTING.md` | Updated quick reference |
| Root `README.md` | Add E2E testing section if not present |

### New Documentation

| File | Purpose |
|------|---------|
| `e2e/ARCHITECTURE.md` | Describe component topology and relationships |

---

## Risk Mitigation

### Risk 1: test-infra upstream changes break CI

**Likelihood**: Low  
**Impact**: High  
**Mitigation**:
- Pin test-infra reference to specific commit SHA
- Add CI job that tests against test-infra@main weekly
- Monitor test-infra releases

### Risk 2: Developer environment differences cause setup failures

**Likelihood**: Medium  
**Impact**: Low  
**Mitigation**:
- Document minimum Docker resource requirements (4GB RAM)
- Provide clear error messages for common failures
- Test setup on both macOS and Linux

---

## Implementation Schedule

| Day | Task | Deliverable |
|-----|------|-------------|
| 1 | Task 1.1 | Root Taskfile.yml with test-infra integration |
| 2 | Task 1.2 | Migrated tasks, consolidated workflow |
| 3 | Task 1.3 | Simplified CI workflow |
| 4 | Task 1.4 | Updated documentation |
| 5 | Testing | End-to-end verification, PR review |

---

## Acceptance Sign-off

Complete when:
- [ ] All user story acceptance criteria met
- [ ] CI workflow passes
- [ ] No regression in existing tests
- [ ] Documentation updated

---

## Appendix A: Current File Structure

```
datum-dns-webhook/
├── Taskfile.yml                    # Root (to be created/enhanced)
├── e2e/
│   ├── Taskfile.yml                # Current E2E tasks (to be consolidated)
│   ├── README.md
│   ├── TESTING.md
│   ├── chainsaw/
│   │   ├── chainsaw-config.yaml
│   │   ├── 01-httproute-creates-record/
│   │   └── 02-txt-ownership-record/
│   ├── manifests/
│   │   ├── dns-operator/           # Mock operator
│   │   ├── dns-fixtures/
│   │   ├── external-dns/
│   │   └── test-app/
│   └── scripts/
│       └── diagnostics.sh
└── .github/
    └── workflows/
        └── e2e.yaml                # To be simplified
```

## Appendix B: Glossary

| Term | Definition |
|------|------------|
| Chainsaw | Kyverno's declarative E2E testing framework |
| DNSRecordSet | Custom resource representing DNS records managed by the DNS Operator |
| DNSZone | Custom resource representing a DNS zone |
| external-dns | Kubernetes controller that creates DNS records from Kubernetes resources |
| KIND | Kubernetes IN Docker - local cluster for testing |
| test-infra | Shared repository providing cluster lifecycle and base infrastructure |
