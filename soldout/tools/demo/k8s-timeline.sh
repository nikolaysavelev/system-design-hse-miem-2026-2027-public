#!/usr/bin/env bash
# k8s-timeline.sh [секунд] — раз в 10 с: реплики soldout и notifier, RPS k6, лаг notifier, CPU PostgreSQL.
# Нужен для make k8s-lag-burst и make k8s-storm: по этой ленте видно, что сделал автоскейлер и что это дало.
set -uo pipefail
DURATION=${1:-120}
KCTX=${KCTX:-k3d-soldout}
PROM=${PROM_URL:-http://localhost:9090}
prom() { curl -sG "$PROM/api/v1/query" --data-urlencode "query=$1" | python3 -c 'import json,sys
r=json.load(sys.stdin)["data"]["result"]; print(round(float(r[0]["value"][1])) if r else 0)' 2>/dev/null || echo "-"; }
ready() { local n; n=$(kubectl --context "$KCTX" get deploy "$1" -o jsonpath='{.status.readyReplicas}' 2>/dev/null); echo "${n:-0}"; }
echo "время      soldout  notifier     RPS     лаг   CPU PG"
end=$(( $(date +%s) + DURATION ))
while [ "$(date +%s)" -lt "$end" ]; do
  pg=$(docker stats --no-stream --format '{{.CPUPerc}}' soldout-postgres-1 2>/dev/null)
  printf '%-9s %8s %9s %7s %7s %8s\n' "$(date +%T)" "$(ready soldout)" "$(ready notifier)" \
    "$(prom 'sum(rate(k6_http_reqs_total[15s]))')" "$(prom 'max(notifier_lag{job="notifier-lag"})')" "${pg:-?}"
  sleep 8
done
