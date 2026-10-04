#!/usr/bin/env bash
# fitness-red.sh — красный пример для make lint-k8s: копия чарта, где у soldout образ с тегом latest и нет
# readinessProbe. Политики обязаны найти ровно эти два нарушения. Копия удаляется.
set -euo pipefail
cd "$(dirname "$0")/../.."
tmp=$(mktemp -d tmp/chart-red.XXXXXX)
trap 'rm -rf "$tmp"' EXIT
cp -R ../deploy/charts/soldout "$tmp/soldout"
python3 - "$tmp/soldout/templates/soldout-deployment.yaml" <<'PY'
import re, sys
p = sys.argv[1]; s = open(p).read()
s, n1 = re.subn(r'image: "\{\{ \$s\.image \}\}:.*\n', 'image: "{{ $s.image }}:latest"\n', s)
s, n2 = re.subn(r'          readinessProbe:\n(?:            .*\n)+', '', s, count=1)
assert n1 == 1 and n2 == 1, "шаблон чарта изменился: поправьте tools/k8s/fitness-red.sh"
open(p, 'w').write(s)
PY
echo "красный пример: soldout:latest и Deployment soldout без readinessProbe"
set +e
out=$(make --no-print-directory lint-k8s CHART="$tmp/soldout" 2>&1); rc=$?
set -e
echo "$out" | grep -E "FAIL|tests,|kubeconform" || true
n=$(echo "$out" | grep -c "^FAIL" || true)
if [ "$rc" -ne 0 ] && [ "$n" -eq 2 ]; then echo "fitness-red-k8s: ok, lint-k8s красный, нарушений ровно 2"; exit 0; fi
echo "fitness-red-k8s: ожидались красный lint-k8s и 2 нарушения, получено rc=$rc, нарушений $n"; echo "$out" | tail -20; exit 1
