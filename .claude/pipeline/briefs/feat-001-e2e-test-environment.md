---
handoff:
  id: feat-001
  from: product-discovery
  to: product-planner
  created: 2026-02-23T00:00:00Z
  context_summary: |
    Consolidate and enhance E2E testing by integrating with test-infra's Taskfile pattern,
    adding Flux-based deployment of external-dns, and planning for real DNS Operator with
    Knot DNS to enable true Gateway-to-DIG verification.
  decisions_made:
    - decision: "Use test-infra's remote Taskfile include pattern rather than duplicating infrastructure code"
      rationale: "test-infra already supports remote inclusion and handles cluster lifecycle. Reduces maintenance burden."
    - decision: "Phase the work into two iterations: consolidation first, then real DNS operator"
      rationale: "Current mock-based tests provide value. Real DNS integration is higher complexity and can follow."
    - decision: "Target both local development and CI as first-class users"
      rationale: "Request explicitly mentions both dev:setup and test:end-to-end workflows. CI already exists but is fragile."
    - decision: "Keep Chainsaw as the test framework"
      rationale: "Recent migration from bash to Chainsaw was completed. Chainsaw provides declarative, debuggable tests."
  open_questions:
    - question: "Should external-dns be deployed via Flux HelmRelease or continue with Kustomize?"
      blocking: false
      context: "Request mentions Flux, but current setup uses kustomize. HelmRelease would be more GitOps-native."
    - question: "What DNS provider should back the real DNS Operator - Knot DNS, CoreDNS, or another?"
      blocking: false
      context: "Request mentions Knot DNS. Need to confirm this is the preferred choice."
    - question: "Should the cluster overlay live in test-infra or in this repository?"
      blocking: false
      context: "test-infra is the shared platform. Component-specific overlays could live in either location."
    - question: "What is the acceptable test execution time budget?"
      blocking: false
      context: "Current CI has 30-minute timeout. Real DNS integration may add latency."
  assumptions:
    - "test-infra repository will remain the canonical source for shared test infrastructure"
    - "DNS Operator APIs are stable enough to depend on directly (not just mock)"
    - "Chainsaw test patterns are working well and don't need replacement"
  platform_capabilities:
    quota: "Not applicable - testing infrastructure, not user-facing feature"
    insights: "Not applicable"
    telemetry: "Existing webhook metrics are tested via Chainsaw assertions on pod readiness"
    activity: "Not applicable"
---

# Discovery Brief: E2E Test Environment with test-infra

**Feature ID**: feat-001
**Discovery Date**: 2026-02-23
**Status**: Ready for Planning

## Problem Statement

The datum-dns-webhook repository has working E2E tests, but the testing infrastructure has several pain points:

1. **Duplicated Infrastructure Code**: The `e2e/Taskfile.yml` duplicates cluster lifecycle management that test-infra already provides. This creates maintenance burden and drift risk.

2. **Fragile CI Integration**: The GitHub Actions workflow manually orchestrates many steps (checkout test-infra, create cluster, wait for components, deploy manifests). Failures in any step require manual debugging.

3. **Mock DNS Operator Limits Confidence**: The current mock operator simply marks all zones/recordsets as "ready" without actually provisioning DNS. This means tests cannot verify actual DNS resolution.

4. **Developer Setup Friction**: A developer must understand they need test-infra cloned next to this repo, then run the right sequence of commands. The test-infra remote-include pattern would simplify this.

The core problem is not that E2E testing doesn't work - it does. The problem is that the setup is more fragile and limited than it needs to be, and future enhancements (real DNS verification) require architectural changes.

## Target Users

### Primary: Webhook Developers (Local Development)
- **Who**: Engineers making changes to datum-dns-webhook
- **Context**: Need to validate changes work before pushing PRs
- **Current pain**: Must manually clone test-infra, understand the Taskfile structure, remember the right sequence of commands
- **Desired experience**: Single `task dev:setup` creates a working environment; `task test:e2e` runs validation

### Primary: CI System (Automated Testing)
- **Who**: GitHub Actions workflows
- **Context**: Runs on every PR and push to main
- **Current pain**: Long YAML file with many orchestration steps; failures are hard to debug
- **Desired experience**: Workflow calls a few high-level tasks; test-infra handles complexity

### Secondary: DNS Operator Developers
- **Who**: Engineers working on the DNS Operator itself
- **Context**: May want to test their changes against external-dns webhook
- **Current pain**: Would need to replace the mock operator manually
- **Desired experience**: Configuration flag to use real operator vs mock

## Scope Boundaries

### In Scope (Phase 1 - Consolidation)

| Item | Description |
|------|-------------|
| test-infra Taskfile integration | Use remote Taskfile include pattern to eliminate duplicate infrastructure code |
| Root Taskfile with dev/test tasks | Create top-level `Taskfile.yml` with `dev:setup` and `test:e2e` tasks |
| Simplified CI workflow | Refactor `.github/workflows/e2e.yaml` to use new task targets |
| Documentation update | Update `e2e/README.md` to reflect new workflow |

### In Scope (Phase 2 - Real DNS)

| Item | Description |
|------|-------------|
| DNS Operator deployment | Deploy actual DNS Operator (not mock) to test cluster |
| DNS Provider integration | Configure Knot DNS (or equivalent) as backing DNS server |
| DIG-based assertions | Add Chainsaw tests that verify actual DNS resolution |
| HTTPRoute-to-DNS validation | Full flow: create HTTPRoute -> verify DNSRecordSet -> verify DIG response |

### Explicitly Out of Scope

| Item | Rationale |
|------|-----------|
| Production DNS configuration | This is test infrastructure only |
| Multi-cluster testing | Single KIND cluster is sufficient for webhook validation |
| Load/performance testing | Functional correctness is the goal, not performance |
| Flux-managed application deployment | The webhook itself will still be deployed via kustomize, not Flux GitOps |
| Changes to test-infra repository | Use existing capabilities; don't require upstream changes |

## Success Criteria

### Phase 1 (Consolidation)

1. **Single-command setup**: Running `task dev:setup` from repo root provisions complete E2E environment
2. **Single-command test**: Running `task test:e2e` executes all Chainsaw tests
3. **CI simplification**: GitHub Actions workflow reduced to <50 lines of YAML
4. **No test regressions**: Existing Chainsaw tests continue to pass
5. **Documentation clarity**: README explains the workflow in <5 minutes reading time

### Phase 2 (Real DNS)

1. **Actual DNS resolution**: Tests can verify `dig whoami.example.com @<knot-dns-ip>` returns expected A record
2. **Full lifecycle coverage**: Create HTTPRoute -> DNSRecordSet created -> DNS resolves -> Delete HTTPRoute -> DNS record removed -> DNS no longer resolves
3. **Reasonable test time**: Full E2E suite completes in <10 minutes locally, <15 minutes in CI

## Platform Capability Assessment

| Capability | Relevance | Notes |
|------------|-----------|-------|
| Quota | Not applicable | Testing infrastructure, not user-facing |
| Insights | Not applicable | No proactive notifications needed |
| Telemetry | Minimal | Webhook already exposes Prometheus metrics; tests verify pod readiness |
| Activity | Not applicable | No audit trail needed for test operations |

## Dependencies

### External Dependencies

| Dependency | Type | Risk |
|------------|------|------|
| datum-cloud/test-infra | Repository | Low - already exists and is actively maintained |
| DNS Operator | Component | Medium - Phase 2 requires stable operator; mock suffices for Phase 1 |
| Knot DNS | Component | Medium - Phase 2 only; need to confirm this is the preferred DNS server |
| Chainsaw | Tool | Low - already in use and working well |

### Internal Dependencies

| Dependency | Type | Risk |
|------------|------|------|
| dns-operator API types | Go module | Low - already imported via `go.miloapis.com/dns-operator/api/v1alpha1` |
| Existing Chainsaw tests | Test suite | Low - will be preserved, not replaced |

## Technical Constraints

1. **KIND cluster limitations**: NodePort services use ports 30000-32767; some features (LoadBalancer) require workarounds
2. **Docker resource usage**: Full E2E environment requires ~4GB RAM and multiple container images
3. **Network isolation**: KIND cluster is isolated; external DNS resolution tests require explicit configuration
4. **CI environment**: GitHub-hosted runners have limited resources; tests must be efficient

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| test-infra changes break webhook CI | Low | High | Pin to specific test-infra ref; monitor upstream changes |
| Real DNS integration adds flakiness | Medium | Medium | Keep mock-based tests as baseline; real DNS tests can be optional |
| Developer environment differences | Medium | Low | Document prerequisites clearly; use containerized tooling where possible |

## Open Questions Requiring Resolution

1. **Flux vs Kustomize for external-dns**: The request mentions Flux, but current deployment uses Kustomize. Should we migrate to HelmRelease for GitOps consistency, or keep the simpler Kustomize approach?

2. **Cluster overlay location**: Should the webhook-specific cluster configuration (DNSZone, DNSRecordSet CRDs, etc.) be contributed to test-infra as a component, or remain in this repository?

3. **Knot DNS confirmation**: The request mentions Knot DNS specifically. Is this the preferred DNS server, or would CoreDNS or another option be acceptable?

4. **Test execution budget**: Current CI timeout is 30 minutes. What is the acceptable test execution time for the enhanced E2E suite?

## Recommended Next Steps

1. **Product Planner**: Formalize this into a specification with detailed task breakdown
2. **Architecture Review**: Confirm test-infra integration approach with platform team
3. **Prioritization**: Decide if Phase 2 (real DNS) is needed for initial release or can follow later

---

*This brief is ready for product-planner to formalize into a specification.*
