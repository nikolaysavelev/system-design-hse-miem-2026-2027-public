# deploy: soldout в Kubernetes

Занятие 5, решение `soldout/docs/adr/ADR-004-runtime-and-rollout.md`. Как пользоваться: `soldout/docs/deploy.md`. Все команды запускаются из `soldout/` (`make k8s-up`, `make k8s-rollout`, `make lint-k8s`).

| Каталог | Что |
|---|---|
| `k3d/cluster.yaml` | кластер k3d `soldout`: 1 server + 2 agent в сети compose-стенда, ingress на порту 8089 |
| `charts/soldout` | Helm-чарт приложений: `soldout` (HPA по CPU) и `notifier` (KEDA по лагу), probes, preStop, PDB |
| `policies` | политики conftest (`*.rego`) и их тесты (`policy_test.rego`) |
| `rbac/agent-ro.yaml` | ServiceAccount агента: только чтение, без секретов |
| `argocd/application.yaml` | Argo CD Application, необязательная часть |

## Границы

В кластере только stateless-приложения. PostgreSQL, PgBouncer, Kafka, Valkey, mailgw и наблюдаемость остаются в docker compose и для чарта являются внешними сервисами (`external.*` и Secret в values). Stateful в кластер несут с оператором и по необходимости; в облаке их место займут managed-сервисы (занятие 8).

## Политики

`make lint-k8s` рендерит чарт (`helm template`), проверяет схему (`kubeconform -strict`) и политики:

| Файл | Правило |
|---|---|
| `image.rego` | у образа фиксированный тег: `latest` и отсутствие тега запрещены |
| `resources.rego` | у каждого контейнера requests и limits по CPU и памяти |
| `probes.rego` | Deployment, на который смотрит Service, имеет readiness и liveness |
| `security.rego` | `runAsNonRoot: true`, `hostNetwork` запрещен |
| `pdb.rego` | у каждого Deployment есть PodDisruptionBudget |

Своя политика: новый файл `policies/<имя>.rego` в пакете `main` с правилом `deny contains msg if { ... }` и тест в `policy_test.rego`. Выборки `deployments`, `docs`, `selects` лежат в `lib.rego`.

## Миграции

На стенде под мигрирует базу при старте (`MIGRATE_ON_START=true`): один под берет advisory-lock `golang-migrate`, остальные ждут. Для прода миграции выносятся в Job перед rollout (hook `pre-upgrade`), а изменения схемы делаются шагами expand и contract. Пример expand: миграция `0022` добавила nullable-колонку `catalog_seat_state.hold_id`, старые поды продолжают работать. Перенос миграций в Job: вариант задания HW3.

## Argo CD (необязательно)

На стенде не устанавливается: кластер и стенд занимают почти все 8 ГБ Docker Desktop. Попробовать:

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml
kubectl apply -f deploy/argocd/application.yaml
kubectl -n argocd port-forward svc/argocd-server 8443:443
```

Идея: желаемое состояние лежит в git, кластер подтягивается к нему сам, ручная правка откатывается (`selfHeal`). Без Argo правило то же: PR → CI (`make lint-k8s`) → `helm upgrade` пайплайном.
