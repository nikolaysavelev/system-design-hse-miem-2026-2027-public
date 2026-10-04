#!/usr/bin/env bash
# Service account Grafana с ролью Viewer и токен для mcp-grafana (занятие 4) → soldout/.env.local (в .gitignore).
# Идемпотентно: рабочий токен из .env.local переиспользуется. Стенд: admin/admin (пароли dev-стенда, spec §6).
set -euo pipefail
cd "$(dirname "$0")/../.."
G=${GRAFANA_URL:-http://localhost:3000}
AUTH=${GRAFANA_ADMIN:-admin:admin}
ENV_FILE=.env.local
SA=mcp-readonly

if [ -f "$ENV_FILE" ] && tok=$(sed -n 's/^GRAFANA_SERVICE_ACCOUNT_TOKEN=//p' "$ENV_FILE") && [ -n "$tok" ] &&
   curl -sf -H "Authorization: Bearer $tok" "$G/api/search?limit=1" >/dev/null; then
  echo "grafana-token: токен в $ENV_FILE рабочий"; exit 0
fi
for i in $(seq 1 30); do curl -sf "$G/api/health" >/dev/null && break; sleep 2; done

id=$(curl -sf -u "$AUTH" "$G/api/serviceaccounts/search?query=$SA" | python3 -c '
import json, sys
print(next((str(a["id"]) for a in json.load(sys.stdin).get("serviceAccounts", []) if a["name"] == "'$SA'"), ""))')
if [ -z "$id" ]; then
  id=$(curl -sf -u "$AUTH" -H 'Content-Type: application/json' -X POST "$G/api/serviceaccounts" \
       -d "{\"name\":\"$SA\",\"role\":\"Viewer\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')
fi
key=$(curl -sf -u "$AUTH" -H 'Content-Type: application/json' -X POST "$G/api/serviceaccounts/$id/tokens" \
      -d "{\"name\":\"mcp-$(date +%s)\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["key"])')
touch "$ENV_FILE"
grep -v '^GRAFANA_SERVICE_ACCOUNT_TOKEN=' "$ENV_FILE" > "$ENV_FILE.tmp" || true
echo "GRAFANA_SERVICE_ACCOUNT_TOKEN=$key" >> "$ENV_FILE.tmp"
mv "$ENV_FILE.tmp" "$ENV_FILE"
echo "grafana-token: service account $SA (Viewer), токен записан в soldout/$ENV_FILE"
