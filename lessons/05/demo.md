# Занятие 5. Демо: все команды по актам

Все команды из `soldout/`. Ветка `demo/l5-k8s`, стенд занятия 5 по умолчанию (`LESSON=05`). Цифры ожиданий: прогоны 02.10.2026 (`results.md`).

## Подготовка (до пары, 15 минут)

```bash
tools/doctor.sh
docker info --format '{{.MemTotal}}'      # нужно ≥ 8 ГБ у Docker Desktop
brew install k3d helm                     # один раз
git switch demo/l5-k8s && make down && make up     # свежая база: 80 000 мест на мероприятие
make k8s-cluster                          # кластер, KEDA, образы: 1.5 минуты (образы KEDA уже в Docker хоста); приложения остаются в compose
make kubeconfig-ro                        # .kube/agent-ro для агента
make pay-demo && make lint-k8s
```

Вкладки: Grafana `soldout-red` (первые две панели: RPS по репликам, число реплик), `soldout-events`, k6; kafka-ui http://localhost:8088. Терминалы: два в `soldout/`, третий с `make k8s-watch` (с 40-й минуты), четвертый с `claude` в `soldout/` (`/mcp`: `kubernetes` connected).

Свежая база обязательна: checkout продает места мероприятия 2, после пяти-шести прогонов они кончаются.

## 0-30. Хвост занятия 4

По `lessons/04/demo.md`, акт 4 и ADR-003. Отличие одно: `make switch` не нужен, все идет на `demo/l5-k8s` через балансировщик `lb` на том же порту 8080.

## Акт 1. Реплики в compose (10 минут)

```bash
make holds-reset                 # шторм сравним только при старте с нуля активных hold
make storm-short                 # 1 реплика: ~2 650 RPS
make docker-stats                # во время шторма: postgres 104-110 %, soldout ~70 % из 200
make pgbouncer-pools             # cl_waiting до 16

make scale SOLDOUT=2             # вторая реплика, lb находит ее по DNS за 5 с
make holds-reset && make storm-short     # ~2 600 RPS: то же самое
make pgbouncer-pools             # cl_active + cl_waiting = 80, sv_active = 20, cl_waiting до 60
make scale-reset
```

Вопрос залу до второго шторма: сколько будет RPS? Ответ: столько же. PostgreSQL занимает свое единственное ядро целиком, вторая реплика принесла 40 клиентов в очередь.

```bash
make scale NOTIFIER=3
make kill-notifier               # notifier стоит 40 с под checkout; в конце "лаг 0 через N с"
make cdc-status                  # три участника группы, по одной партиции на каждого
make scale-reset
```

Чистое время разбора очереди (если есть 4 минуты): `make kill-notifier NOTIFIER_DOWN=75` при `NOTIFIER=1` (72 с) и `NOTIFIER=3` (32 с). Иначе показать `act1/README.md`.

Сказать про два дефекта, которые нашли реплики (`results.md`, раздел "Что нашли реплики"): до исправления три реплики notifier работали как одна, а карта зала расходилась с истиной.

## Акт 2. Кластер и выкатка (10 минут)

```bash
make k8s-deploy                  # приложения переезжают в кластер: compose-реплики останавливаются, 20 с
kubectl get pods -o wide
make k8s-status                  # deploy, hpa, scaledobject, pdb, ingress
make pay-demo                    # тот же сценарий через ingress на 8089
```

Показать `deploy/charts/soldout/values-k3d.yaml`: адреса PostgreSQL, Kafka, Valkey внешние для кластера. Показать `templates/soldout-deployment.yaml`: `maxUnavailable: 0`, probes, `preStop`.

Терминал 3: `make k8s-watch`.

```bash
# терминал 1                              # терминал 2, через 10-15 с после старта
CHECKOUT_DURATION=60s make checkout       make k8s-rollout NO_READINESS=1
```

Итог k6: 2 500-4 100 ошибок (3.8-5.4 %). Вопрос: какая из двух выкаток их дала? Вторая: она останавливает поды без preStop.

```bash
make k8s-rollout                          # вернуть probes, без нагрузки
CHECKOUT_DURATION=60s make checkout       make k8s-rollout
```

Итог k6: 0 ошибок. Откат: `helm --kube-context k3d-soldout history soldout`, `helm rollback`.

## Акт 3. Автоскейлинг (10 минут)

```bash
make k8s-lag-burst      # 90 с checkout; на 20-й секунде notifier на паузе (0 реплик), на 60-й пауза снята
```

В `make k8s-watch`: notifier 0 → 3, после нулевого лага через минуту 2 и 1. В конце лента "время, реплики, RPS, лаг" и сверка: paid = notifications, дублей 0. Занимает около 4 минут; пока идет, показать `templates/scaledobject.yaml` и сказать про потолок в три партиции.

```bash
make k8s-storm          # шторм 90 с: HPA soldout 1 → 2 → 3
```

Лента в конце: RPS 3 000 при одной реплике, 2 400-2 600 при трех, PostgreSQL 104-107 %. Сравнение на одной реплике: `make k8s-storm SOLDOUT_MAX=1` или таблица в `act3/README.md`.

Если времени нет: `k8s-storm` не запускать, показать `act3/storm-hpa.txt`.

## Агент и кластер (8 минут)

```bash
make break-oom          # лимит 96 МиБ, шторм 15 с: OOMKilled, CrashLoopBackOff
```

Терминал с агентом, промпт 1 из `ai-session.md`: "soldout в кластере падает под нагрузкой, разберись и предложи исправление, кластер менять нельзя". Ждем `describe pod`, `events`, `logs --previous`, вывод про лимит и `GOMEMLIMIT`, правку `values.yaml` в git.

```bash
kubectl --kubeconfig .kube/agent-ro apply -f ../deploy/rbac/agent-ro.yaml    # Forbidden
make restore-k8s
```

Промпт 2: "добавь в чарт сервис mailgw как Deployment", затем `make lint-k8s`. Если агент все сделал правильно с первого раза:

```bash
make fitness-red-k8s    # копия чарта с latest и без readiness: ровно два нарушения
make lint-k8s           # зеленый: kubeconform, тесты политик 9 из 9, conftest 6 из 6
```

## ADR-004 (6 минут)

`soldout/docs/adr/ADR-004-runtime-and-rollout.md`: таблица A/B/C, решение с обоснованием, known limitations. Вслух: HPA по CPU для монолита бесполезен, пока узкое место в базе; 3 × 40 соединений на 20 серверных.

## Итоги и HW3 (6 минут)

HW2 выдается на занятии 7, HW3 на занятии 10. Тизер периметра (занятие 8): ingress открыт всем, кто вообще может дергать `/v1/holds`.

## После пары

```bash
make down               # удаляет и кластер
```

## Если что-то пошло не так

| Симптом | Что делать |
|---|---|
| `make k8s-cluster`: "сначала make up" | кластер подключается к сети стенда: `make up`, затем снова |
| поды не видят базу | `kubectl run nc --rm -i --restart=Never --image=busybox:1.36 -- nc -zv -w 3 pgbouncer 6432`; если закрыто, `make down && make up K8S=1` |
| `make checkout` в режиме k8s ходит не туда | `cat tmp/stand-mode` должен быть `k8s`; `make k8s-deploy` |
| после правки кода в кластере старая версия | `make k8s-images && make k8s-deploy` |
| HPA показывает `<unknown>` | metrics-server собирает первые метрики около минуты после старта пода |
| кластер не помещается в память | `make k8s-down`, акты 2-3 по `act2/README.md` и `act3/README.md` |
| вернуться в compose | `make up` (удаляет релиз из кластера, 30 с) |
