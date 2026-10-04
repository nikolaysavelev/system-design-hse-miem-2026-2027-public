#!/usr/bin/env bash
# kubeconfig-ro.sh — kubeconfig агента с правами только на чтение (ServiceAccount agent-ro) в .kube/agent-ro.
# Токен живет 24 часа: после занятия доступ истекает сам. Файл в .gitignore.
set -euo pipefail
cd "$(dirname "$0")/../.."
KCTX=${KCTX:-k3d-soldout}
OUT=.kube/agent-ro
kubectl --context "$KCTX" apply -f ../deploy/rbac/agent-ro.yaml >/dev/null
cluster=$(kubectl config view --raw -o jsonpath="{.contexts[?(@.name==\"$KCTX\")].context.cluster}")
server=$(kubectl config view --raw -o jsonpath="{.clusters[?(@.name==\"$cluster\")].cluster.server}")
ca=$(kubectl config view --raw -o jsonpath="{.clusters[?(@.name==\"$cluster\")].cluster.certificate-authority-data}")
token=$(kubectl --context "$KCTX" create token agent-ro --duration=24h)
mkdir -p .kube
umask 077
cat > "$OUT" <<YAML
apiVersion: v1
kind: Config
clusters:
  - name: soldout
    cluster:
      server: $server
      certificate-authority-data: $ca
users:
  - name: agent-ro
    user:
      token: $token
contexts:
  - name: agent-ro
    context:
      cluster: soldout
      user: agent-ro
      namespace: default
current-context: agent-ro
YAML
echo "kubeconfig агента: soldout/$OUT (только чтение, токен на 24 часа)"
echo "проверка: чтение $(kubectl --kubeconfig "$OUT" auth can-i list pods), запись $(kubectl --kubeconfig "$OUT" auth can-i create deployments || true), секреты $(kubectl --kubeconfig "$OUT" auth can-i get secrets || true)"
