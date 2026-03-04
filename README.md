# Datum DNS Webhook Provider for ExternalDNS

A webhook provider for
[ExternalDNS](https://github.com/kubernetes-sigs/external-dns) that manages DNS
records through [Datum Cloud DNS](https://github.com/datum-cloud/dns-operator)
custom resources (`DNSZone` and `DNSRecordSet`).

## Architecture

```
ExternalDNS → Webhook (sidecar) → DNSRecordSet CRs → DNS Operator → DNS Provider
```

The webhook runs as a sidecar container alongside ExternalDNS and:

1. Discovers managed domains by watching `DNSZone` resources
2. Translates ExternalDNS endpoints into `DNSRecordSet` custom resources
3. Routes records to the correct zone using longest-suffix domain matching
4. Filters domains so ExternalDNS only manages hostnames with a matching zone
5. Tracks ownership via labels to prevent conflicts between instances

## Installation

### Using the ExternalDNS Helm Chart

The webhook is deployed as a sidecar via the ExternalDNS Helm chart:

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: external-dns
  namespace: external-dns
spec:
  chart:
    spec:
      chart: external-dns
      sourceRef:
        kind: HelmRepository
        name: external-dns
        namespace: flux-system
  values:
    sources:
      - gateway-httproute
      - crd
    provider:
      name: webhook
      webhook:
        image:
          repository: ghcr.io/datum-cloud/external-dns-webhook
          tag: v0.1.0
        args:
          - --owner-id=my-external-dns
          - --log-level=info
    rbac:
      additionalPermissions:
        - apiGroups: ["dns.networking.miloapis.com"]
          resources: ["dnszones"]
          verbs: ["get", "list", "watch"]
        - apiGroups: ["dns.networking.miloapis.com"]
          resources: ["dnsrecordsets"]
          verbs: ["get", "list", "watch", "create", "update", "delete"]
    txtOwnerId: my-external-dns
```

### Using the Kustomize Bundle

```bash
kubectl apply -k https://ghcr.io/datum-cloud/external-dns-webhook/deploy:v0.1.0
```

### Building from Source

```bash
task build    # Build the binary
task test     # Run unit tests
task docker   # Build Docker image
```

## Configuration

### Command-Line Flags

| Flag | Description | Default | Required |
|------|-------------|---------|----------|
| `--owner-id` | Unique identifier for this ExternalDNS instance | - | Yes |
| `--namespace` | Specific namespace to watch (empty = all namespaces) | - | No |
| `--namespace-label-selector` | Label selector to filter namespaces | - | No |
| `--kubeconfig` | Path to kubeconfig file (empty = in-cluster config) | - | No |
| `--config` | Path to YAML config file for additional zone sources | - | No |
| `--port` | HTTP server port for webhook API | `8888` | No |
| `--bind-address` | Address to bind the webhook HTTP server | `0.0.0.0` | No |
| `--metrics-port` | Port for Prometheus metrics and health checks | `8080` | No |
| `--log-level` | Log level (debug, info, warn, error) | `info` | No |
| `--dry-run` | Dry-run mode: do not make actual DNS changes | `false` | No |

### Single-Cluster (Flags Only)

For the common case of a single cluster, CLI flags are all you need:

```bash
# Watch all namespaces
--owner-id=my-external-dns

# Watch a specific namespace
--owner-id=my-external-dns --namespace=production

# Watch namespaces by label
--owner-id=my-external-dns --namespace-label-selector=dns-managed=true
```

When running in-cluster (the default), the webhook uses the pod's service
account. Use `--kubeconfig` only for out-of-cluster development.

### Multi-Cluster (Config File)

For managing zones across multiple control planes, provide a YAML config file
via `--config`. The config file defines zone sources — each pointing to a
different cluster:

```yaml
# zone-sources.yaml
zoneSources:
  - name: production
    kubeconfig: /etc/kubeconfigs/prod.kubeconfig
    refreshInterval: 30s
  - name: staging
    kubeconfig: /etc/kubeconfigs/staging.kubeconfig
    namespace: dns-zones
    refreshInterval: 2m
  - name: dev
    kubeconfig: /etc/kubeconfigs/dev.kubeconfig
    namespaceLabelSelector: "dns=enabled"
```

```bash
--owner-id=my-external-dns --config=/etc/datum-dns/zone-sources.yaml
```

When `--config` is provided, zone sources come from the file. When omitted, a
single default zone source is created from the `--namespace`,
`--namespace-label-selector`, and `--kubeconfig` flags.

#### Zone Source Fields

| Field | Description | Default |
|-------|-------------|---------|
| `name` | Identifier for this source (required) | - |
| `kubeconfig` | Path to kubeconfig (empty = in-cluster) | - |
| `namespace` | Specific namespace to watch | all |
| `namespaceLabelSelector` | Label selector for namespace filtering | - |
| `refreshInterval` | How often to re-discover zones | `60s` |

## How It Works

### Zone Discovery

The webhook periodically lists `DNSZone` resources to build a domain filter.
Only hostnames matching a known zone are managed:

```yaml
apiVersion: dns.networking.miloapis.com/v1alpha1
kind: DNSZone
metadata:
  name: example-com
  namespace: default
spec:
  domainName: example.com
  dnsZoneClassName: cloudflare
```

### Record Routing

When multiple zones exist (e.g. `example.com` and `sub.example.com`), the
webhook uses longest-suffix matching to route records to the most specific zone.
`app.sub.example.com` routes to the `sub.example.com` zone, not `example.com`.

### DNSRecordSet Creation

ExternalDNS endpoints are translated into `DNSRecordSet` resources placed in the
same namespace as the matching `DNSZone`:

```yaml
apiVersion: dns.networking.miloapis.com/v1alpha1
kind: DNSRecordSet
metadata:
  name: www-example-com-a-abc123
  namespace: default
  labels:
    external-dns.io/owner: my-external-dns
    external-dns.io/resource: www.example.com
    external-dns.io/record-type: A
    external-dns.io/managed-by: datum-cloud-webhook
spec:
  dnsZoneRef:
    name: example-com
  recordType: A
  records:
  - name: www
    ttl: 300
    a:
      content: 192.0.2.1
```

### Ownership Tracking

Records are labeled with the `--owner-id` value (`external-dns.io/owner`) to
prevent conflicts between multiple ExternalDNS instances. The webhook only
modifies records it owns.

## RBAC Requirements

The webhook requires permissions to watch zones and manage record sets:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: datum-dns-webhook
rules:
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["dns.networking.miloapis.com"]
  resources: ["dnszones"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["dns.networking.miloapis.com"]
  resources: ["dnsrecordsets"]
  verbs: ["get", "list", "watch", "create", "update", "delete"]
```

## Health Checks

- **Liveness**: `GET /healthz` on metrics port (always returns 200 OK)
- **Readiness**: `GET /readyz` on metrics port (returns 200 OK once zones are
  discovered, with a 30s grace period on startup)

## Metrics

Prometheus metrics are exposed at `/metrics` on the metrics port (default 8080):

| Metric | Type | Description |
|--------|------|-------------|
| `datum_dns_zones_discovered` | Gauge | Number of DNSZone resources discovered |
| `datum_dns_recordsets_managed` | Gauge | Number of DNSRecordSet resources managed |
| `datum_dns_operations_total` | Counter | Total DNS operations (create/update/delete) |
| `datum_dns_translation_errors_total` | Counter | Endpoint translation errors |
| `datum_dns_ownership_conflicts_total` | Counter | Ownership conflicts detected |
| `datum_dns_http_requests_total` | Counter | HTTP requests to the webhook |
| `datum_dns_http_request_duration_seconds` | Histogram | HTTP request latency |

## Development

### Running Tests

```bash
task test     # Unit tests with race detection
task lint     # golangci-lint
```

### E2E Testing

End-to-end tests run in a KIND cluster with a mock DNS operator, ExternalDNS,
and the webhook. Tests use [Chainsaw](https://kyverno.github.io/chainsaw/).

```bash
task dev:setup    # Create KIND cluster + deploy everything
task test:e2e     # Run chainsaw tests
task e2e:logs     # View component logs
task e2e:diag     # Collect diagnostics
task e2e:cleanup  # Tear down
```

The suite covers:

| Test | Validates |
|------|-----------|
| HTTPRoute → A record | Create and delete lifecycle |
| TXT ownership records | Ownership tracking alongside A records |
| Multiple routes | Independent DNSRecordSets per route |
| Zone routing | Longest-suffix match across multiple zones |
| Unknown domain filtered | No records for unmanaged domains |
| DNSEndpoint CRD | CRD source → record lifecycle |

## License

Apache License 2.0
