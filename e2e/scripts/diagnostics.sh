#!/usr/bin/env bash
set -euo pipefail

OUTPUT_DIR="${OUTPUT_DIR:-./e2e-diagnostics}"
mkdir -p "$OUTPUT_DIR"

log() {
  echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

log "Collecting diagnostics to: $OUTPUT_DIR"

# Cluster info
log "Collecting cluster info..."
kubectl cluster-info dump > "$OUTPUT_DIR/cluster-info.txt" 2>&1 || true

# All pods
log "Collecting pod status..."
kubectl get pods --all-namespaces -o wide > "$OUTPUT_DIR/pods.txt" 2>&1 || true

# Webhook logs
log "Collecting webhook logs..."
kubectl logs -n external-dns -l app=datum-dns-webhook --tail=500 > "$OUTPUT_DIR/webhook-logs.txt" 2>&1 || true

# ExternalDNS logs
log "Collecting ExternalDNS logs..."
kubectl logs -n external-dns -l app=external-dns --tail=500 > "$OUTPUT_DIR/external-dns-logs.txt" 2>&1 || true

# DNS resources
log "Collecting DNS resources..."
kubectl get dnszones,dnsrecordsets,dnszoneclasses --all-namespaces -o yaml > "$OUTPUT_DIR/dns-resources.yaml" 2>&1 || true

# HTTPRoutes
log "Collecting Gateway API resources..."
kubectl get gateways,httproutes --all-namespaces -o yaml > "$OUTPUT_DIR/gateway-resources.yaml" 2>&1 || true

# Events
log "Collecting events..."
kubectl get events --all-namespaces --sort-by='.lastTimestamp' > "$OUTPUT_DIR/events.txt" 2>&1 || true

# Webhook metrics
log "Collecting webhook metrics..."
kubectl exec -n external-dns deploy/datum-dns-webhook -- wget -q -O- http://localhost:8080/metrics > "$OUTPUT_DIR/webhook-metrics.txt" 2>&1 || true

log "Diagnostics collected in: $OUTPUT_DIR"
