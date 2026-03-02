# Phase 1 E2E Tests

This document describes the Phase 1 Chainsaw E2E tests implemented for datum-dns-webhook.

## Overview

Phase 1 tests validate the **actual reconciliation flow** of ExternalDNS with the datum-dns-webhook provider. Each test starts with a clean state, creates resources, waits for reconciliation, asserts expected outcomes, and cleans up.

## Test Structure

Tests are organized in numbered directories that execute in sequence:

```
e2e/chainsaw/
├── 01-httproute-creates-record/
│   ├── chainsaw-test.yaml      # Test definition
│   └── httproute.yaml           # HTTPRoute manifest
├── 02-txt-ownership-record/
│   ├── chainsaw-test.yaml
│   └── httproute.yaml
└── chainsaw-config.yaml         # Global config (2m timeout)
```

## Test 1: HTTPRoute Creates and Deletes A Record

**File**: `01-httproute-creates-record/chainsaw-test.yaml`

**Purpose**: Verify that creating an HTTPRoute with a hostname causes a DNSRecordSet to be created, and deleting the HTTPRoute removes the DNSRecordSet.

**Flow**:
1. **Assert Clean State**: Verify no DNSRecordSet exists with label `external-dns.io/owner: external-dns-e2e` and name matching `test-*`
2. **Create HTTPRoute**: Apply HTTPRoute named `test-httproute` with:
   - Hostname: `test-app.example.com`
   - Annotation: `external-dns.alpha.kubernetes.io/hostname: test-app.example.com`
   - ParentRef: `default-gateway` in `envoy-gateway-system`
   - BackendRef: `whoami` service on port 80
3. **Assert DNSRecordSet Created** (60s timeout):
   - Label: `external-dns.io/owner: external-dns-e2e`
   - Spec: `dnsZoneRef.name: example-com`
   - Spec: `recordType: A`
   - Spec: `records[0].name: test-app`
4. **Delete HTTPRoute**: Remove `test-httproute`
5. **Assert DNSRecordSet Removed** (60s timeout): Verify DNSRecordSet no longer exists

**Validates**:
- HTTPRoute → DNSRecordSet creation flow
- Correct DNSZone reference (example-com)
- Correct record name (test-app from hostname)
- Cleanup on HTTPRoute deletion

## Test 2: TXT Ownership Record Created

**File**: `02-txt-ownership-record/chainsaw-test.yaml`

**Purpose**: Verify that ExternalDNS creates TXT records for ownership tracking alongside A records.

**Flow**:
1. **Assert Clean State**: Verify no DNSRecordSet exists with name matching `test-txt-*`
2. **Create HTTPRoute**: Apply HTTPRoute with hostname `test-txt.example.com`
3. **Assert TXT Record Created** (60s timeout):
   - Label: `external-dns.io/owner: external-dns-e2e`
   - Spec: `recordType: TXT`
   - Spec: `records[0].name: test-txt`
4. **Assert A Record Created** (60s timeout):
   - Label: `external-dns.io/owner: external-dns-e2e`
   - Spec: `recordType: A`
   - Spec: `records[0].name: test-txt`
5. **Cleanup**: Delete HTTPRoute
6. **Assert Both Records Removed** (60s timeout)

**Validates**:
- TXT ownership record creation (ExternalDNS txt-owner-id)
- Both TXT and A records created for same hostname
- Both records cleaned up on deletion

## Key Configuration

### Global Timeouts (chainsaw-config.yaml)
- Assert: 2 minutes (default for all assertions)
- Apply: 30 seconds
- Delete: 30 seconds
- Error: 30 seconds

### External DNS Configuration
- Owner ID: `external-dns-e2e`
- Source: `gateway-httproute`
- Provider: `webhook` (datum-dns-webhook)
- Interval: 30 seconds
- TXT Registry: Enabled (default)

### Test Resources
- DNSZone: `example-com` (domainName: `example.com`)
- Gateway: `default-gateway` in `envoy-gateway-system` namespace
- Backend Service: `whoami` in `default` namespace

## Running Tests

```bash
# Run all tests
chainsaw test e2e/chainsaw/

# Run specific test
chainsaw test e2e/chainsaw/01-httproute-creates-record/

# Run with verbose output
chainsaw test e2e/chainsaw/ --test-dir e2e/chainsaw --config e2e/chainsaw/chainsaw-config.yaml
```

## What These Tests Validate

✅ **Reconciliation Flow**: Tests wait for actual reconciliation, not just static resource existence
✅ **Clean State**: Each test verifies no conflicting resources exist before starting
✅ **Creation**: DNSRecordSets are created with correct spec and labels
✅ **Deletion**: DNSRecordSets are removed when source resources are deleted
✅ **Ownership**: TXT records track ownership via txt-owner-id
✅ **Multi-Record**: Both A and TXT records created for same hostname

## What These Tests Don't Cover (Future Phases)

- Multiple HTTPRoutes to same hostname (conflict resolution)
- Invalid configurations (missing DNSZone, invalid hostnames)
- Update scenarios (changing hostname, changing targets)
- Cross-namespace resources
- Different record types (CNAME, AAAA)
- Gateway API integration details (load balancer IPs, gateway status)

## Troubleshooting

### Test Hangs on Assert
- Check ExternalDNS logs: `kubectl logs -n external-dns -l app=external-dns`
- Check webhook logs: `kubectl logs -n external-dns -l app=datum-dns-webhook`
- Verify Gateway exists: `kubectl get gateway -n envoy-gateway-system`
- Verify DNSZone exists: `kubectl get dnszone -n default`

### DNSRecordSet Not Created
- Verify HTTPRoute has correct annotation
- Verify Gateway parentRef is valid
- Check ExternalDNS interval (30s default)
- Verify webhook connectivity: `kubectl get svc -n external-dns datum-dns-webhook`

### Cleanup Fails
- Manual cleanup: `kubectl delete httproute -n default test-httproute test-txt-httproute`
- Manual cleanup: `kubectl delete dnsrecordset -n default -l external-dns.io/owner=external-dns-e2e`
