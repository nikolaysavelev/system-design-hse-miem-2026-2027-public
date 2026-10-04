#!/usr/bin/env bash
# Регистрация коннектора Debezium (идемпотентно): роль в PostgreSQL, топики, PUT конфигурации в Connect REST.
set -euo pipefail
cd "$(dirname "$0")/../.."
COMPOSE=${COMPOSE:-docker compose -f docker-compose.yml -f compose/lesson-02.yml -f compose/lesson-04.yml}
CONNECT=${CONNECT_URL:-http://localhost:8083}
$COMPOSE exec -T postgres psql -q -U soldout -d soldout -v ON_ERROR_STOP=1 -f - < tools/cdc/debezium-role.sql
COMPOSE="$COMPOSE" tools/cdc/topics.sh
for i in $(seq 1 60); do curl -sf "$CONNECT/connectors" >/dev/null && break; sleep 2; done
name=$(python3 -c 'import json; print(json.load(open("compose/debezium/outbox-connector.json"))["name"])')
# Connect отвечает 409, пока перебалансирует задачи (например, после пересоздания PostgreSQL): повторяем
for i in $(seq 1 10); do
  python3 -c 'import json; print(json.dumps(json.load(open("compose/debezium/outbox-connector.json"))["config"]))' |
    curl -sf -X PUT -H 'Content-Type: application/json' --data @- "$CONNECT/connectors/$name/config" >/dev/null && break
  [ "$i" = 10 ] && { echo "connector $name: Connect не принял конфигурацию"; exit 1; }
  sleep 3
done
# задача могла упасть на прошлой конфигурации — перезапускаем только упавшие
curl -sf -X POST "$CONNECT/connectors/$name/restart?includeTasks=true&onlyFailed=true" >/dev/null || true
for i in $(seq 1 30); do
  state=$(curl -sf "$CONNECT/connectors/$name/status" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["connector"]["state"], *[t["state"] for t in d["tasks"]])' 2>/dev/null || true)
  case "$state" in "RUNNING RUNNING"*) echo "connector $name: $state"; exit 0 ;; esac
  sleep 2
done
echo "connector $name не перешёл в RUNNING: $state"; curl -s "$CONNECT/connectors/$name/status"; exit 1
