#!/bin/bash
set -e

# Validate Kustomize configurations
# This script verifies all kustomization overlays build successfully

echo "Validating datum-dns-webhook Kustomize configurations..."
echo ""

CONFIGS=(
  "config/base"
  "config/dependencies/external-dns"
  "config/overlays/test/dns-operator"
  "config/overlays/test/dns-fixtures"
  "config/overlays/test/test-app"
  "config/overlays/test"
)

FAILED=0

for config in "${CONFIGS[@]}"; do
  echo -n "Validating $config... "
  if kubectl kustomize "$config" > /dev/null 2>&1; then
    echo "✓"
  else
    echo "✗ FAILED"
    FAILED=$((FAILED + 1))
  fi
done

echo ""
if [ $FAILED -eq 0 ]; then
  echo "✓ All configurations validated successfully!"
  exit 0
else
  echo "✗ $FAILED configuration(s) failed validation"
  exit 1
fi
