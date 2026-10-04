#!/usr/bin/env bash
# Предварительно скачивает все образы курса (например, под VPN или через зеркало).
# Использование: infra/pull-all.sh [images.txt]
set -euo pipefail
LIST="${1:-$(dirname "$0")/images.txt}"
grep -vE '^\s*(#|$)' "$LIST" | while read -r img; do
  echo "== pull $img"
  docker pull "$img"
done
echo "Готово. Проверка digest'ов: infra/pin-images.sh --check"
