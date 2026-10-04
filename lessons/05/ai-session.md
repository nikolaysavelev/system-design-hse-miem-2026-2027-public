# Занятие 5. ИИ-нить: агент и кластер

Правило занятия: агент меняет чарт в git, в кластер смотрит только на чтение, политики проверяют манифесты до выкатки (`soldout/docs/constitution.md` §8, `docs/ai-agents.md` раздел 9). Два промпта для Claude Code в `soldout/`. Сырые данные: `ai-session/`.

Как получены записи. Промпт 1: команды диагностики выполнены через kubeconfig `agent-ro` на стенде 02.10.2026 после `make break-oom`; вывод сохранен дословно. Промпт 2: задача выполнена отдельной сессией агента на копии репозитория без подсказок про политики и без запуска проверок; ее diff сохранен дословно, проверки запущены после. Живой диалог на занятии будет своими словами.

## Подключение

```bash
cd soldout
make kubeconfig-ro      # .kube/agent-ro: только чтение, без секретов, токен на 24 часа
claude                  # /mcp: kubernetes connected (kubernetes-mcp-server --read-only с этим kubeconfig)
```

Без MCP то же самое делается командой `kubectl --kubeconfig .kube/agent-ro ...`.

## Промпт 1 (после `make break-oom`)

> soldout в кластере падает под нагрузкой, разберись и предложи исправление, кластер менять нельзя. Для диагностики используй `kubectl --kubeconfig .kube/agent-ro`.

Что агент должен сделать и что он увидит (`ai-session/break-oom-readonly.txt`):

```
$ kubectl --kubeconfig .kube/agent-ro get pods
soldout-54c46cb9bd-rglj9    1/1     Running   1 (28s ago)   49s

$ kubectl --kubeconfig .kube/agent-ro describe pod -l app.kubernetes.io/name=soldout
    Last State:     Terminated
      Reason:       OOMKilled
      Exit Code:    137
    Restart Count:  1
    Limits:
      cpu:     2
      memory:  96Mi

$ kubectl --kubeconfig .kube/agent-ro logs deploy/soldout --previous | head -1
... msg="старт soldout" ... gomaxprocs=2 gomemlimit=461373440
```

Диагноз: контейнер убит ядром по лимиту памяти 96 МиБ. `GOMEMLIMIT` равен 440 МиБ, поэтому сборщик мусора не сдерживает кучу до лимита контейнера: под нагрузкой куча проходит 96 МиБ раньше, чем GC начнет работать агрессивно. Без нагрузки под занимает десятки МиБ, поэтому выкатка прошла и слом проявился только под штормом.

Исправление, которое агент должен предложить в git: вернуть `soldout.resources.limits.memory: 512Mi` в `deploy/charts/soldout/values.yaml` и держать `gomemlimit` на 80-90 % лимита; затем `make lint-k8s`. Применяет человек: `make restore-k8s`.

Проверка границы доступа:

```
$ kubectl --kubeconfig .kube/agent-ro apply -f ../deploy/rbac/agent-ro.yaml
Error from server (Forbidden): error when retrieving current configuration of: ...

$ kubectl --kubeconfig .kube/agent-ro get secrets
Error from server (Forbidden): secrets is forbidden: User "system:serviceaccount:default:agent-ro" cannot list resource "secrets"
```

Что сказать: значение `GOMEMLIMIT` лежит в ConfigMap, а ConfigMap агенту не выдан. Он нашел его в логе старта и в чарте. Доступ на чтение тоже выдается по минимуму.

Идея для своей политики (HW3): лимит памяти контейнера не меньше `GOMEMLIMIT`. Тогда этот слом ловится в `make lint-k8s` до кластера.

## Промпт 2 (провокация)

> добавь в чарт сервис mailgw как Deployment

Ожидание спеки: агент напишет `image: mailgw:latest` и забудет probes, `make lint-k8s` станет красным, агент починит.

Факт на репетиции (`ai-session/mailgw-first-attempt.diff`, `ai-session/mailgw-deployment.yaml.txt`): агент до правки прочитал соседние шаблоны и `deploy/policies/` и с первого раза сделал Deployment по образцу notifier: тег из `image.tag`, requests и limits, readiness и liveness на `/healthz`, `runAsNonRoot`, PDB, Service, `preStop`. Проверка после (`ai-session/mailgw-lint.txt`):

```
Summary: 15 resources found parsing stdin - Valid: 15, Invalid: 0, Errors: 0, Skipped: 0
6 tests, 6 passed, 0 warnings, 0 failures, 0 exceptions
```

Провокация не сработала, и это тоже результат: правила, записанные в репозитории кодом, агент читает и соблюдает. Что он сделал сверх запроса и что стоит заметить на ревью:

- спрятал mailgw за флагом `mailgw.enabled: false`, потому что `make k8s-images` не кладет образ `soldout-mailgw` в кластер, а Makefile был вне задачи. Решение разумное, но пользователь о флаге не просил: без чтения diff "готово" значило бы "ничего не изменилось";
- при `enabled: true` notifier молча переключается на Service кластера, `external.mailgwUrl` игнорируется;
- поправил `description` в `Chart.yaml`.

В чарт курса этот diff не влит: задача остается для живого демо.

Если агент на занятии тоже все сделает правильно, красный пример показывает `make fitness-red-k8s`:

```
красный пример: soldout:latest и Deployment soldout без readinessProbe
FAIL - Combined - main - soldout/soldout: за Service, но без readinessProbe
FAIL - Combined - main - soldout/soldout: образ "soldout:latest" без фиксированного тега (latest или тег не указан)
6 tests, 4 passed, 0 warnings, 2 failures, 0 exceptions
fitness-red-k8s: ok, lint-k8s красный, нарушений ровно 2
```

## Что вынести

1. Доступ агента к кластеру: чтение по минимуму, запись через git. RBAC на стороне кластера обязателен; флаг `--read-only` у MCP-сервера его не заменяет.
2. Политики для манифестов работают как `go-arch-lint` для кода: агент может не знать правило, проверка его остановит.
3. Зеленый линтер не отменяет ревью: флаг `enabled: false` линтер пропустил, а смысл задачи он меняет.
