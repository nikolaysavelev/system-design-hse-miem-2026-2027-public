#!/usr/bin/env bash
# Генерирует/проверяет infra/images.lock: image:tag <пробел> sha256-digest.
# Использование: infra/pin-images.sh            — перегенерировать lock из локально скачанных образов
#                infra/pin-images.sh --check    — сверить локальные образы с lock (exit 1 при расхождении)
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
LIST="$DIR/images.txt"; LOCK="$DIR/images.lock"
MODE="${1:-generate}"

digest_of() {
  docker image inspect --format '{{index .RepoDigests 0}}' "$1" 2>/dev/null | sed 's/.*@//'
}

if [ "$MODE" = "--check" ]; then
  rc=0
  while read -r img want; do
    [ -z "$img" ] && continue; case "$img" in \#*) continue;; esac
    have="$(digest_of "$img" || true)"
    if [ -z "$have" ]; then echo "[MISS] $img не скачан"; rc=1
    elif [ "$have" != "$want" ]; then echo "[DIFF] $img: local=$have lock=$want"; rc=1
    else echo "[ OK ] $img"; fi
  done < "$LOCK"
  exit $rc
fi

{
  echo "# Сгенерировано infra/pin-images.sh $(date -u +%Y-%m-%dT%H:%MZ). Формат: image:tag digest"
  grep -vE '^\s*(#|$)' "$LIST" | while read -r img; do
    d="$(digest_of "$img" || true)"
    if [ -z "$d" ]; then echo "образ $img не скачан, сначала infra/pull-all.sh" >&2; exit 1; fi
    echo "$img $d"
  done
} > "$LOCK"
cat "$LOCK"
