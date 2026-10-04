# Развертывание soldout в Kubernetes (занятие 5, ADR-004)

Приложения `soldout` и `notifier` живут в кластере, PostgreSQL, PgBouncer, Kafka, Valkey, mailgw и наблюдаемость остаются в docker compose. Для кластера это внешние сервисы: в чарте только их адреса. Все команды из `soldout/`.

## Что нужно на хосте

Docker (8 ГБ памяти у Docker Desktop хватает), `kubectl`, `k3d`, `helm`:

```bash
brew install k3d helm        # macOS; Linux: https://k3d.io, https://helm.sh
```

`kubeconform` и `conftest` для `make lint-k8s` запускаются в контейнерах, ставить их не нужно.

## Два режима стенда

| | compose (по умолчанию) | k8s |
|---|---|---|
| Где приложения | compose, профиль `apps`: `lb` (nginx) → `soldout`, `notifier` | кластер k3d `soldout`, чарт `deploy/charts/soldout` |
| Вход | http://localhost:8080 | http://localhost:8089 (ingress traefik) |
| Как включить | `make up` | `make up K8S=1` или `make k8s-up` на поднятом стенде |
| Реплики | `make scale SOLDOUT=2 NOTIFIER=3` | HPA (soldout), KEDA (notifier) |

Режим записан в `tmp/stand-mode`; `make checkout`, `make pay-demo`, `make trace-last` сами ходят по нужному адресу. Набор приложений у стенда один: `make k8s-deploy` останавливает compose-приложения, `make up` удаляет релиз из кластера и возвращает их в compose.

```bash
make up                # стенд: stateful + приложения в compose
make k8s-cluster       # кластер, KEDA, образы: 1.5-2 минуты; приложения в compose не трогает
make k8s-deploy        # приложения переезжают в кластер: 20-30 с
make k8s-status        # deploy, hpa, scaledobject, pdb, pods, ingress
make up                # назад в compose
make down              # все удалить, кластер тоже
```

`make k8s-up` = `k8s-cluster` + `k8s-deploy`. С нуля `make up K8S=1` занимает около 3 минут.

## Как поды ходят в compose

Узлы k3d подключены к сети compose `soldout_default` (`deploy/k3d/cluster.yaml`, `network:`). CoreDNS кластера пересылает неизвестные имена в DNS Docker, поэтому под обращается к `pgbouncer:6432`, `postgres:5432` (миграции), `kafka:9092`, `valkey:6379`, `otel-collector:4317`, `mailgw:8090` так же, как контейнер compose. Адреса лежат в `values-k3d.yaml`, строки подключения к базе в Secret `soldout-secrets`.

Второй путь, через хост: `values-k3d-host.yaml` (`host.k3d.internal` и опубликованные порты compose, для Kafka отдельный listener `EXTERNAL://:29092`). Так выглядит настоящий внешний managed-сервис. На Docker Desktop этот путь идет через прокси хоста и вдвое медленнее: checkout 685 RPS против 1 147, pay p99 1.04 с против 0.72 с.

```bash
make k8s-deploy HELM_EXTRA="-f ../deploy/charts/soldout/values-k3d-host.yaml"
```

Проверка сети из пода, если что-то не подключается:

```bash
kubectl run nc --rm -i --restart=Never --image=busybox:1.36 -- sh -c 'nc -zv -w 3 pgbouncer 6432; nc -zv -w 3 kafka 9092'
```

Кластер живет в сети стенда, поэтому `make down` сначала удаляет кластер, а `make k8s-cluster` требует поднятого стенда.

## Образы

`make k8s-images` собирает образы тем же Dockerfile, что и compose, и кладет их в узлы (`k3d image import`): реестр на стенде не нужен. Образ один на три бинарника, у notifier в чарте `command: ["/notifier"]`.

## Выкатка

```bash
make k8s-rollout                  # новый тег, RollingUpdate maxUnavailable 0, readiness + preStop
make k8s-rollout NO_READINESS=1   # демо: две версии подряд без readiness и preStop
make k8s-rollout TAG=v2
```

Что защищает от ошибок:

| Механизм | От чего |
|---|---|
| `startupProbe` и `readinessProbe` на `/readyz` | трафик в под, который еще мигрирует или не подключился к базе |
| `maxUnavailable: 0`, `maxSurge: 1` | остановка старого пода до готовности нового; плохая версия не вытесняет рабочую |
| `preStop: sleep 5` | запросы в под, который уже получил SIGTERM, но еще числится в endpoints |
| мягкая остановка (`SHUTDOWN_TIMEOUT` 10 с, `terminationGracePeriodSeconds` 30) | обрыв начатых запросов |
| PDB | drain узла, уводящий все реплики сразу |

Откат:

```bash
helm --kube-context k3d-soldout history soldout
helm --kube-context k3d-soldout rollback soldout            # на предыдущую ревизию
make restore-k8s                                            # к значениям из git
```

Миграции идут при старте пода (`MIGRATE_ON_START=true`): первый под мигрирует, остальные ждут advisory-lock. В проде это Job перед rollout, а схема меняется шагами expand и contract, потому что во время выкатки с базой работают обе версии кода.

## Автоскейлинг

```bash
make k8s-watch                    # HPA, ScaledObject, поды раз в 2 с
make k8s-lag-burst                # notifier на паузе 40 с под checkout: KEDA 0 → 3 → 1
make k8s-storm                    # шторм 90 с: HPA soldout 1 → 3, лента RPS и CPU PostgreSQL
make k8s-storm SOLDOUT_MAX=1      # тот же шторм на одной реплике для сравнения
```

- **notifier, KEDA по лагу.** Триггер `kafka`: `consumerGroup: notifier`, `topic: outbox.event.order`, `lagThreshold: 200`. Желаемое число реплик = лаг / 200, не больше 3 и не больше числа партиций.
- **soldout, HPA по CPU.** 70 % от request 500m. Под штормом HPA поднимает 3 реплики, RPS не растет: узкое место в PostgreSQL (ADR-004, ограничение 1).

`make holds-reset` удаляет активные hold между штормами: прогоны сравнимы только при старте с нуля.

## Проверки и агент

```bash
make lint-k8s            # kubeconform -strict + conftest (deploy/policies/*.rego, тесты политик), входит в lint-all
make fitness-red-k8s     # копия чарта с latest и без readiness: ровно два нарушения
make kubeconfig-ro       # .kube/agent-ro: ServiceAccount agent-ro, только чтение, без секретов, токен на 24 часа
make break-oom           # лимит памяти 96 МиБ + шторм 15 с: OOMKilled; make restore-k8s возвращает
```

Агент меняет чарт в git и запускает `make lint-k8s`. В кластер он ничего не применяет; для диагностики у него `kubectl --kubeconfig .kube/agent-ro` или Kubernetes MCP в режиме `--read-only` (`.mcp.json`). Подробно: `../docs/ai-agents.md`, раздел 9.

## Что где лежит

```
deploy/k3d/cluster.yaml            кластер: 1 server + 2 agent, сеть стенда, ingress на 8089
deploy/charts/soldout/             чарт: Deployment soldout и notifier, Service, Ingress, ConfigMap, Secret, HPA, ScaledObject, PDB
deploy/charts/soldout/values*.yaml значения: по умолчанию, стенд (k3d), вариант через хост
deploy/policies/                   политики conftest и их тесты
deploy/rbac/agent-ro.yaml          доступ агента только на чтение
deploy/argocd/application.yaml     Argo CD, необязательная часть
```
