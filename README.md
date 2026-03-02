# Datum DNS Webhook Provider for ExternalDNS

This is a webhook provider for
[ExternalDNS](https://github.com/kubernetes-sigs/external-dns) that manages DNS
records through Datum Cloud DNS custom resources (`DNSZone` and `DNSRecordSet`).

## Overview

The Datum DNS webhook provider enables ExternalDNS to manage DNS records by
creating and updating Kubernetes custom resources rather than directly
interacting with a DNS provider API. This approach allows for:

- **Declarative DNS management** through Kubernetes CRDs
- **Multi-tenancy** support via namespace isolation
- **Audit trails** through Kubernetes event logs
- **RBAC integration** for fine-grained access control

## Architecture

```
ExternalDNS → Datum DNS Webhook → DNSRecordSet CRs → Datum DNS Operator → DNS Provider
```

The webhook provider:
1. Watches `DNSZone` resources to discover managed domains
2. Converts ExternalDNS endpoints to `DNSRecordSet` custom resources
3. Provides domain filtering based on available zones
4. Tracks ownership through labels to prevent conflicts

## Installation

### Prerequisites

- Kubernetes cluster with ExternalDNS installed
- Datum DNS CRDs installed (`DNSZone` and `DNSRecordSet`)
- Appropriate RBAC permissions (see below)

### Building from Source

```bash
# Build the binary
task build

# Run tests
task test

# Build Docker image
task docker
```

### Running Locally

```bash
go run main.go \
  --owner-id=my-external-dns \
  --namespace=default \
  --kubeconfig=~/.kube/config \
  --log-level=debug
```

## Configuration

### Command-Line Flags

| Flag | Description | Default | Required |
|------|-------------|---------|----------|
| `--owner-id` | Unique identifier for this ExternalDNS instance | - | Yes |
| `--namespace` | Specific namespace to watch (empty = all namespaces) | - | No |
| `--namespace-label-selector` | Label selector to filter namespaces | - | No |
| `--kubeconfig` | Path to kubeconfig file (empty = in-cluster config) | - | No |
| `--port` | HTTP server port for webhook API | 8888 | No |
| `--bind-address` | Address to bind the webhook HTTP server | 127.0.0.1 | No |
| `--metrics-port` | Port for Prometheus metrics and health checks | 8080 | No |
| `--log-level` | Log level (debug, info, warn, error) | info | No |
| `--dry-run` | Dry-run mode: do not make actual DNS changes | false | No |

### Namespace Watching Modes

The webhook provider supports three namespace watching modes:

1. **All Namespaces** (default): Watches all namespaces in the cluster
   ```bash
   --owner-id=my-external-dns
   ```

2. **Specific Namespace**: Watches only a single namespace
   ```bash
   --owner-id=my-external-dns --namespace=production
   ```

3. **Label Selector**: Watches namespaces matching a label selector
   ```bash
   --owner-id=my-external-dns --namespace-label-selector=dns-managed=true
   ```

## Usage with ExternalDNS

### ExternalDNS Configuration

Configure ExternalDNS to use the webhook provider:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: external-dns
spec:
  template:
    spec:
      containers:
      - name: external-dns
        image: registry.k8s.io/external-dns/external-dns:v0.15.0
        args:
        - --source=service
        - --source=ingress
        - --provider=webhook
        - --webhook-provider-url=http://datum-dns-webhook:8888
        - --txt-owner-id=my-external-dns
```

### Webhook Provider Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: datum-dns-webhook
  namespace: external-dns
spec:
  replicas: 1
  selector:
    matchLabels:
      app: datum-dns-webhook
  template:
    metadata:
      labels:
        app: datum-dns-webhook
    spec:
      serviceAccountName: datum-dns-webhook
      containers:
      - name: webhook
        image: datum-dns-webhook:latest
        args:
        - --owner-id=my-external-dns
        - --log-level=info
        ports:
        - name: webhook
          containerPort: 8888
        - name: metrics
          containerPort: 8080
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: datum-dns-webhook
  namespace: external-dns
spec:
  selector:
    app: datum-dns-webhook
  ports:
  - name: webhook
    port: 8888
    targetPort: 8888
  - name: metrics
    port: 8080
    targetPort: 8080
```

## RBAC Requirements

The webhook provider requires the following permissions:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: datum-dns-webhook
  namespace: external-dns
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: datum-dns-webhook
rules:
# Watch and list namespaces (for namespace filtering)
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list", "watch"]
# Manage DNSZone resources
- apiGroups: ["dns.datum.net"]
  resources: ["dnszones"]
  verbs: ["get", "list", "watch"]
# Manage DNSRecordSet resources
- apiGroups: ["dns.datum.net"]
  resources: ["dnsrecordsets"]
  verbs: ["get", "list", "watch", "create", "update", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: datum-dns-webhook
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: datum-dns-webhook
subjects:
- kind: ServiceAccount
  name: datum-dns-webhook
  namespace: external-dns
```

## How It Works

### DNSZone Discovery

The webhook watches `DNSZone` resources to determine which domains it should
manage:

```yaml
apiVersion: dns.datum.net/v1alpha1
kind: DNSZone
metadata:
  name: example-com
  namespace: production
spec:
  domainName: example.com
  dnsZoneClassName: cloudflare
```

### DNSRecordSet Creation

When ExternalDNS requests a DNS record, the webhook creates a `DNSRecordSet`:

```yaml
apiVersion: dns.datum.net/v1alpha1
kind: DNSRecordSet
metadata:
  name: www-example-com-a-abc123
  namespace: production
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

Records are labeled with the `owner-id` to prevent conflicts between multiple
ExternalDNS instances. The webhook will only modify records it owns.

## Metrics

Prometheus metrics are exposed on the metrics port (default: 8080) at
`/metrics`:

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `datum_dns_zones_discovered` | Gauge | `namespace` | Number of DNSZone resources discovered |
| `datum_dns_recordsets_managed` | Gauge | `namespace`, `record_type` | Number of DNSRecordSet resources managed |
| `datum_dns_operations_total` | Counter | `operation`, `status` | Total number of DNS operations |
| `datum_dns_translation_errors_total` | Counter | `error_type` | Total number of endpoint translation errors |
| `datum_dns_ownership_conflicts_total` | Counter | - | Total number of ownership conflicts |
| `datum_dns_http_requests_total` | Counter | `method`, `path`, `status` | Total number of HTTP requests |
| `datum_dns_http_request_duration_seconds` | Histogram | `method`, `path` | HTTP request duration |

## Health Checks

- **Liveness**: `GET /healthz` on metrics port (always returns 200 OK)
- **Readiness**: `GET /readyz` on metrics port (returns 200 OK if zones
  discovered or within 30s grace period)

## API Endpoints

The webhook implements the ExternalDNS webhook protocol:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Returns domain filter (content-type negotiation) |
| `/records` | GET | Returns current DNS records |
| `/records` | POST | Applies DNS record changes |
| `/adjustendpoints` | POST | Adjusts endpoints before planning |

## Development

### Running Tests

```bash
# Run unit tests
task test

# Run with coverage
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### E2E Testing

End-to-end tests validate the complete integration with ExternalDNS and the
Datum DNS Operator in a KIND cluster.

**Prerequisites:**
- Docker
- [Task](https://taskfile.dev)
- [Chainsaw](https://kyverno.github.io/chainsaw/)

**Quick Start:**

```bash
# Setup E2E environment and run tests
task dev:setup
task test:e2e

# Cleanup when done
task e2e:cleanup
```

**Available E2E Tasks:**

- `task dev:setup` - Create complete E2E environment
- `task test:e2e` - Run E2E tests
- `task e2e:logs` - View component logs
- `task e2e:diag` - Collect diagnostics
- `task e2e:cleanup` - Destroy environment

See `e2e/README.md` for detailed E2E testing documentation.

### Linting

```bash
task lint
```

## Troubleshooting

### No Zones Discovered

If the webhook reports no zones discovered:

1. Check that `DNSZone` resources exist in watched namespaces
2. Verify RBAC permissions allow listing zones
3. Check namespace filtering configuration

### Ownership Conflicts

If you see ownership conflict errors:

1. Ensure each ExternalDNS instance has a unique `owner-id`
2. Check for manually created `DNSRecordSet` resources with conflicting labels
3. Verify the correct `owner-id` is set in both ExternalDNS and the webhook

### Records Not Created

If DNS records are not being created:

1. Enable debug logging: `--log-level=debug`
2. Check webhook logs for translation errors
3. Verify the domain matches a `DNSZone`
4. Ensure RBAC permissions allow creating `DNSRecordSet` resources

## License

This project is licensed under the Apache License 2.0 - see the original
ExternalDNS project for details.

## Contributing

Contributions are welcome! Please ensure:

- All tests pass
- Code is formatted with `gofmt`
- Linting passes with `golangci-lint`
- Commits follow conventional commit format
