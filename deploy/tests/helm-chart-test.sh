#!/bin/sh
set -eu
chart=deploy/helm/sub2api-public-transit
for values in deploy/helm/examples/*.yaml; do
  helm lint "$chart" -f "$values"
  helm template transit "$chart" -f "$values" > /tmp/ai-transit-helm-rendered.yaml
  if grep -Eq 'kind: PersistentVolumeClaim|REDIS_|ADMIN_PASSWORD|JWT_SECRET|/app/data|httpGet:' /tmp/ai-transit-helm-rendered.yaml; then exit 1; fi
done
if helm template transit "$chart" >/dev/null 2>&1; then echo 'missing required values accepted'; exit 1; fi
