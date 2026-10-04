# soldout — команды стенда. По умолчанию — стенд занятия 5 (LESSON=05: оверлеи lesson-02 + lesson-04 + lesson-05,
# приложения за балансировщиком lb, профиль apps). K8S=1: приложения в кластере k3d, в compose остается stateful.
# Занятие 4: LESSON=04; занятие 2: LESSON=02 / make up-02; занятие 1: make up-01 / LESSON=01.
SHELL := /bin/bash
LESSON ?= 05
ifeq ($(LESSON),05)
COMPOSE_OVERLAYS ?= -f compose/lesson-02.yml -f compose/lesson-04.yml -f compose/lesson-05.yml
else ifeq ($(LESSON),04)
COMPOSE_OVERLAYS ?= -f compose/lesson-02.yml -f compose/lesson-04.yml
else
COMPOSE_OVERLAYS ?= -f compose/lesson-$(LESSON).yml
endif
PROFILES ?= --profile obs
# занятие 4: 80 000 мест на мероприятие — три checkout-storm за занятие продают ~45 000 мест мероприятия 2
ifeq ($(LESSON),04)
export SEED_SEATS_PER_EVENT ?= 80000
PROFILES := --profile obs --profile cdc
endif
ifeq ($(LESSON),05)
export SEED_SEATS_PER_EVENT ?= 80000
# режим стенда: compose (приложения в compose за lb) или k8s (приложения в k3d); пишут make up и make k8s-deploy
MODE := $(shell cat tmp/stand-mode 2>/dev/null)
PROFILES := --profile obs --profile cdc $(if $(or $(K8S),$(filter k8s,$(MODE))),,--profile apps)
endif
# адрес приложения с хоста и из контейнера k6: в режиме k8s это ingress кластера (с хоста порт 8089,
# из контейнера k6 балансировщик k3d в той же сети Docker)
ifeq ($(MODE),k8s)
HOST_URL ?= http://localhost:8089
export K6_BASE_URL ?= http://k3d-soldout-serverlb:80
else
HOST_URL ?= http://localhost:8080
endif
ifeq ($(LESSON),01)
PROFILES :=
endif
COMPOSE := docker compose -f docker-compose.yml $(COMPOSE_OVERLAYS)
COMPOSE_OBS := $(COMPOSE) $(PROFILES)
GOBIN ?= $(shell go env GOPATH)/bin
ARCH_LINT := $(GOBIN)/go-arch-lint
K6_OUT := $(if $(filter 02 04 05,$(LESSON)),-o experimental-prometheus-rw,)
# --no-deps: k6 не трогает стенд. Без него compose пересоздаёт soldout с дефолтным окружением
# и молча отменяет make break-* (стрельба идёт уже не по той конфигурации).
K6_RUN := $(COMPOSE) --profile load run --rm --no-deps k6 run $(K6_OUT)
PSQL := $(COMPOSE) exec -T postgres psql -U soldout -d soldout
RESULTS := k6/results
# checkout (занятие 4) идёт на мероприятии 2, шторм — на мероприятии 1: конкуренция за пул, а не за места
CHECKOUT_EVENT := 10000000-0000-4000-8000-000000000002
K6_RUN_CHECKOUT := $(COMPOSE) --profile load run --rm --no-deps -e EVENT_ID=$(CHECKOUT_EVENT) -e EVENT_NO=2 k6 run $(K6_OUT)
STEP ?= $(shell git rev-parse --abbrev-ref HEAD | tr '/' '-')
export VERSION ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: help build up up-01 up-02 up-lite up-obs down logs ps seed psql switch switch-apply \
        smoke storm storm-short hot-row waiting-room holds-any-demo queue-status checkout checkout-storm pay-demo \
        invariants invariants-local lint-arch lint-arch-install lint-migrations lint-semgrep lint-asyncapi fitness-red-semgrep lint-all test test-unit vet fmt \
        pprof-top pprof-window pprof-heap pg-top pg-reset pg-activity explain-seatstates results docker-stats pgbouncer-pools \
        break-cache break-admission break-pool break-prepared break-goroutine restore \
        trace-last traces-slow trace-json grafana-token cdc-status cdc-register kill-notifier poison dlq up-poll repair-i5 \
        up-05 scale scale-reset k8s-cluster k8s-images k8s-deploy k8s-up k8s-rollout k8s-rollout-step k8s-lag-burst k8s-storm holds-reset kubeconfig-ro break-oom restore-k8s lint-k8s fitness-red-k8s k8s-undeploy k8s-down k8s-status k8s-watch

help:
	@echo "Стенд:      up (=up-05) | up-02 | up-01 | up-lite | down | switch BRANCH=demo/l4-otel | logs | ps | psql"
	@echo "Нагрузка:   smoke | storm (2 мин) | storm-short (60 с) | hot-row | waiting-room | holds-any-demo"
	@echo "Занятие 4:  checkout (150 VU, 90 с) | checkout-storm (= checkout WITH_STORM=1) | pay-demo"
	@echo "Трейсы:     trace-last | traces-slow [Q=TraceQL] | trace-json TRACE=<id> | grafana-token"
	@echo "Проверки:   invariants | lint-arch | lint-migrations | lint-all | test | test-unit | results"
	@echo "Профиль:    pprof-top | pprof-heap | pg-top | pg-reset | pg-activity | explain-seatstates | docker-stats"
	@echo "События:    cdc-status | cdc-register | dlq | repair-i5 | up-poll (вариант A: polling без Debezium)"
	@echo "Занятие 5:  scale SOLDOUT=n NOTIFIER=m | scale-reset | holds-reset | up K8S=1 | k8s-cluster | k8s-deploy | k8s-status | k8s-watch"
	@echo "            k8s-rollout [NO_READINESS=1] | k8s-lag-burst | k8s-storm [SOLDOUT_MAX=1] | break-oom | restore-k8s | k8s-down"
	@echo "            lint-k8s | fitness-red-k8s | kubeconfig-ro   (docs/deploy.md)"
	@echo "Сломы:      break-cache | break-admission | break-pool | break-prepared | kill-notifier | poison | restore"

build:
	$(COMPOSE) build soldout postgres

up: up-$(LESSON)

up-01:
	$(MAKE) up-stack LESSON=01

up-02:
	$(MAKE) up-stack LESSON=02

up-04:
	$(MAKE) up-stack LESSON=04
	@COMPOSE="$(COMPOSE)" tools/cdc/register.sh
	@-tools/grafana/sa-token.sh >/dev/null 2>&1 || echo "grafana-token: Grafana недоступна, токен для mcp-grafana не создан (make grafana-token)"

up-05:
ifeq ($(K8S),1)
	-@$(COMPOSE) --profile apps rm -sf lb soldout notifier >/dev/null 2>&1
	$(MAKE) up-stack LESSON=05 K8S=1
	$(MAKE) k8s-up
else
	@if $(HELM) status soldout >/dev/null 2>&1; then $(HELM) uninstall soldout --wait >/dev/null && echo "релиз soldout в кластере удален: приложения возвращаются в compose"; fi
	@mkdir -p tmp && echo compose > tmp/stand-mode
	$(MAKE) up-stack LESSON=05 MODE=compose
	@COMPOSE="$(COMPOSE)" tools/cdc/register.sh
endif
	@-tools/grafana/sa-token.sh >/dev/null 2>&1 || echo "grafana-token: Grafana недоступна, токен для mcp-grafana не создан (make grafana-token)"

up-stack:
	$(COMPOSE_OBS) up -d --build --wait
	@echo "готово: $(HOST_URL)/readyz  Grafana: http://localhost:3000/d/soldout-red  Pyroscope: http://localhost:4040"

up-lite:
	$(MAKE) up-stack COMPOSE_OVERLAYS="-f compose/lesson-02.yml -f compose/lesson-04.yml -f compose/lesson-05.yml -f compose/lite.yml" PROFILES="--profile apps" SEED_SEATS_PER_EVENT=4000

up-obs: up-05

# кластер k3d живет в сети стенда, поэтому удаляется первым
down:
	-@k3d cluster list $(K3D_CLUSTER) >/dev/null 2>&1 && $(MAKE) k8s-down
	$(COMPOSE) --profile load --profile tools --profile obs --profile cdc --profile apps down -v --remove-orphans

# make switch BRANCH=demo/l2-stepA — переключить ветку и пересобрать сервис (данные сохраняются).
# Только вперёд по шагам: если схема в БД новее миграций целевой ветки, нужен make down && make up.
# Собирается только образ soldout: пересборка postgres меняет ID образа и пересоздаёт контейнер БД.
switch:
	@test -n "$(BRANCH)" || (echo "укажите BRANCH=demo/l2-stepA"; exit 1)
	@have=$$($(PSQL) -Atc "SELECT version FROM schema_migrations" 2>/dev/null || true); \
	want=$$(git ls-tree -r --full-tree --name-only $(BRANCH) | sed -n 's|^soldout/migrations/0*\([0-9]*\)_.*\.up\.sql$$|\1|p' | sort -n | tail -1); \
	if [ -n "$$have" ] && [ -n "$$want" ] && [ "$$have" -gt "$$want" ]; then \
		echo "схема в БД версии $$have, в $(BRANCH) миграции до $$want: назад switch не умеет."; \
		echo "выполните: make down && git switch $(BRANCH) && make up"; exit 1; \
	fi
	git switch $(BRANCH)
	@# дальше — рецепт из Makefile целевой ветки: у неё свои сервисы, профили и шаги после старта
	$(MAKE) switch-apply

# выполняется после git switch Makefile'ом новой ветки: пересобрать наши образы (не postgres), поднять всё, донастроить
switch-apply:
	$(COMPOSE_OBS) build $$($(COMPOSE_OBS) config --services | grep -vx postgres | tr '\n' ' ')
	$(COMPOSE_OBS) up -d --wait
	@if [ -x tools/cdc/register.sh ] && [ "$(LESSON)" != 02 ] && [ "$(LESSON)" != 01 ]; then COMPOSE="$(COMPOSE)" tools/cdc/register.sh; fi
	@curl -s $(HOST_URL)/readyz && echo

logs:
	$(COMPOSE) logs -f soldout

ps:
	$(COMPOSE_OBS) ps

psql:
	$(COMPOSE) exec postgres psql -U soldout -d soldout

seed:
	$(COMPOSE) exec -T postgres psql -U soldout -d soldout -v ON_ERROR_STOP=1 \
		-c "SET soldout.seats_per_event = '$${SEED_SEATS_PER_EVENT:-40000}'" -f - < seed/seed.sql

# ---------- занятие 5: реплики в compose ----------
# make scale SOLDOUT=2 — вторая реплика монолита за lb; make scale NOTIFIER=3 — три потребителя группы notifier.
# lb находит реплики по DNS Docker за 5 с. Любой make up / restore / break-* возвращает по одной реплике.
SOLDOUT ?= 1
NOTIFIER ?= 1
scale:
	$(COMPOSE_OBS) up -d --no-recreate --wait --scale soldout=$(SOLDOUT) --scale notifier=$(NOTIFIER)
	@sleep 6; $(COMPOSE) ps --format '{{.Service}}\t{{.Name}}\t{{.Status}}' soldout notifier lb

scale-reset:
	$(MAKE) scale SOLDOUT=1 NOTIFIER=1

# ---------- занятие 5: Kubernetes (k3d). Подробно: ../deploy/README.md, docs/deploy.md ----------
DEPLOY := ../deploy
CHART := $(DEPLOY)/charts/soldout
K3D_CLUSTER := soldout
KCTX := k3d-$(K3D_CLUSTER)
KUBECTL := kubectl --context $(KCTX)
HELM := helm --kube-context $(KCTX)
KEDA_VERSION ?= 2.21.0
KEDA_IMAGES := ghcr.io/kedacore/keda:$(KEDA_VERSION) ghcr.io/kedacore/keda-metrics-apiserver:$(KEDA_VERSION) ghcr.io/kedacore/keda-admission-webhooks:$(KEDA_VERSION)
TAG ?= lesson-5
# свои строки подключения для подов: K8S_DATABASE_URL, K8S_DATABASE_URL_DIRECT, K8S_NOTIFIER_DATABASE_URL в .env.local
HELM_SECRETS := $(shell [ -f .env.local ] && sed -n 's/^K8S_\([A-Z_]*DATABASE_URL[A-Z_]*\)=\(.*\)/--set-string secrets.data.\1=\2/p' .env.local)
HELM_VALUES = -f $(CHART)/values-k3d.yaml --set image.tag=$(TAG) $(HELM_SECRETS) $(HELM_EXTRA)

# кластер, KEDA и образы: медленная часть (2-3 минуты), делается до занятия; compose-приложения не трогает
k8s-cluster:
	@command -v k3d >/dev/null && command -v helm >/dev/null || { echo "нужны k3d и helm: brew install k3d helm"; exit 1; }
	@docker network inspect soldout_default >/dev/null 2>&1 || { echo "сначала make up: кластер подключается к сети стенда soldout_default"; exit 1; }
	@k3d cluster list $(K3D_CLUSTER) >/dev/null 2>&1 || k3d cluster create --config $(DEPLOY)/k3d/cluster.yaml
	@$(HELM) repo add kedacore https://kedacore.github.io/charts >/dev/null 2>&1 || true
	@# образы KEDA один раз скачиваются в Docker хоста и кладутся в узлы: новый кластер не зависит от скорости ghcr.io
	@for i in $(KEDA_IMAGES); do docker image inspect $$i >/dev/null 2>&1 || docker pull -q $$i; done
	@k3d image import $(KEDA_IMAGES) -c $(K3D_CLUSTER) >/dev/null 2>&1 || echo "k3d image import образов KEDA не удался: кластер скачает их сам"
	$(HELM) upgrade --install keda kedacore/keda --version $(KEDA_VERSION) -n keda --create-namespace --wait --timeout 8m >/dev/null
	$(MAKE) k8s-images
	@$(KUBECTL) get nodes

# образы собирает compose (тот же Dockerfile), k3d кладет их в узлы: реестр на стенде не нужен
k8s-images:
	$(COMPOSE) build soldout notifier
	@[ "$(TAG)" = lesson-5 ] || { docker tag soldout:lesson-5 soldout:$(TAG); docker tag soldout-notifier:lesson-5 soldout-notifier:$(TAG); }
	k3d image import soldout:$(TAG) soldout-notifier:$(TAG) -c $(K3D_CLUSTER)

# приложения переезжают в кластер: у стенда один набор приложений, compose-реплики останавливаются
k8s-deploy:
	-@$(COMPOSE) --profile apps rm -sf lb soldout notifier >/dev/null 2>&1
	@mkdir -p tmp && echo k8s > tmp/stand-mode
	@echo "helm upgrade --install soldout (тег $(TAG))"; $(HELM) upgrade --install soldout $(CHART) $(HELM_VALUES) --wait --timeout 4m 2> >(grep -v warnings.go >&2) | grep -E "STATUS|REVISION"
	@COMPOSE="$(COMPOSE)" tools/cdc/register.sh
	@$(KUBECTL) get pods -o wide
	@echo "готово: http://localhost:8089/readyz (ingress кластера); назад в compose: make up"

k8s-up: k8s-cluster k8s-deploy

# Выкатка новой версии под нагрузкой (в другом терминале make checkout). Образ тот же, тег новый: rollout настоящий.
#   make k8s-rollout                  readiness + preStop: 0 ошибок
#   make k8s-rollout NO_READINESS=1   две версии подряд без readiness и preStop. Первая заменяет поды, у которых
#                                     preStop еще есть, и может пройти чисто; вторая останавливает поды без паузы:
#                                     балансировщик еще шлет в них запросы, а процесс уже не слушает
#   make k8s-rollout TAG=v2           свой тег
k8s-rollout:
	@$(MAKE) --no-print-directory k8s-rollout-step ROLLOUT_TAG=$(if $(filter lesson-5,$(TAG)),r$$(date +%H%M%S),$(TAG))
ifeq ($(NO_READINESS),1)
	@$(MAKE) --no-print-directory k8s-rollout-step ROLLOUT_TAG=$(if $(filter lesson-5,$(TAG)),r$$(date +%H%M%S),$(TAG))b
endif
	@$(KUBECTL) get pods -l app.kubernetes.io/name=soldout -o custom-columns=POD:.metadata.name,IMAGE:.spec.containers[0].image,READY:.status.containerStatuses[0].ready,STARTED:.status.startTime

k8s-rollout-step:
	@tag=$(ROLLOUT_TAG); \
	docker tag soldout:lesson-5 soldout:$$tag && docker tag soldout-notifier:lesson-5 soldout-notifier:$$tag && \
	k3d image import soldout:$$tag soldout-notifier:$$tag -c $(K3D_CLUSTER) >/dev/null 2>&1 && \
	echo ">>> rollout $$tag$(if $(NO_READINESS), БЕЗ readiness и preStop,) $$(date +%T)" && \
	$(HELM) upgrade soldout $(CHART) $(HELM_VALUES) --set image.tag=$$tag $(if $(NO_READINESS),--set soldout.probes.enabled=false) --wait --timeout 3m 2> >(grep -v warnings.go >&2) >/dev/null && \
	$(KUBECTL) rollout status deploy/soldout --timeout 120s >/dev/null && echo ">>> готово $$(date +%T)"; rc=$$?; \
	docker rmi soldout:$$tag soldout-notifier:$$tag >/dev/null 2>&1; exit $$rc

# ---------- занятие 5: автоскейлинг ----------
# KEDA по лагу: notifier ставится на паузу (0 реплик) на NOTIFIER_DOWN секунд под checkout, лаг растет до тысяч.
# После снятия паузы KEDA поднимает реплики по числу партиций (до 3), лаг уходит, через минуту остается одна.
k8s-lag-burst:
	@test "$(MODE)" = k8s || { echo "приложения не в кластере: make k8s-deploy"; exit 1; }
	@mkdir -p $(RESULTS); rm -f $(RESULTS)/.notifier-start; \
	tools/demo/k8s-timeline.sh 280 > $(RESULTS)/k8s-lag-burst-timeline.txt & tl=$$!; \
	(sleep 20; echo ">>> notifier: пауза KEDA, 0 реплик"; $(KUBECTL) annotate scaledobject notifier autoscaling.keda.sh/paused-replicas=0 --overwrite >/dev/null; \
	 sleep $(NOTIFIER_DOWN); echo ">>> пауза снята, лаг $$($(LAG))"; $(KUBECTL) annotate scaledobject notifier autoscaling.keda.sh/paused-replicas- >/dev/null; date +%s > $(RESULTS)/.notifier-start) & \
	$(K6_RUN_CHECKOUT) --summary-export=/results/checkout-lag-burst-$(STEP).json /scripts/checkout.js; rc=$$?; wait %2; \
	echo "ждём, пока notifier догонит…"; \
	until [ "$$($(LAG))" = "0" ]; do sleep 1; done; \
	echo "лаг 0 через $$(( $$(date +%s) - $$(cat $(RESULTS)/.notifier-start) )) с после снятия паузы; ждём возврата к одной реплике…"; \
	until [ "$$($(KUBECTL) get deploy notifier -o jsonpath='{.status.replicas}')" = "1" ]; do sleep 3; done; sleep 10; \
	kill $$tl 2>/dev/null; cat $(RESULTS)/k8s-lag-burst-timeline.txt; \
	$(PSQL) -c "SELECT (SELECT count(*) FROM orders WHERE status = 'paid') AS paid, (SELECT count(*) FROM notifications WHERE kind = 'tickets_issued') AS notifications, (SELECT count(*) FROM (SELECT payload->>'order_id' FROM notifications WHERE kind = 'tickets_issued' GROUP BY 1 HAVING count(*) > 1) d) AS duplicates, (SELECT count(*) FROM notifications WHERE order_id IS NOT NULL AND status <> 'sent') AS notifier_not_sent;"; \
	exit $$rc

# Сравнимость штормов: прогон стартует с нулем активных hold, иначе растет доля 409 и цифры не сопоставить.
# Ждать TTL 10 минут на занятии некогда: активные hold и их строки проекции удаляются, версии секторов растут.
holds-reset:
	@$(PSQL) -q -c "DELETE FROM holds WHERE status = 'active'" -c "DELETE FROM admissions"
	@sleep 5   # шины реплик дочитывают события прошлого прогона, иначе они вернут строки в проекцию
	@$(PSQL) -q -c "DELETE FROM catalog_seat_state c WHERE c.state = 'held' AND NOT EXISTS (SELECT 1 FROM holds h WHERE h.event_id = c.event_id AND h.seat_id = c.seat_id AND h.status = 'active')" \
		-c "UPDATE catalog_sector_version SET version = version + 1, updated_at = now()"
	@echo "holds-reset: активных hold $$($(PSQL) -Atc "SELECT count(*) FROM holds WHERE status = 'active'")"

# HPA по CPU: шторм 90 с через ingress, HPA поднимает soldout до SOLDOUT_MAX. Лента показывает, что RPS при
# этом не растет: узкое место в PostgreSQL. make k8s-storm SOLDOUT_MAX=1 — тот же шторм на одной реплике.
SOLDOUT_MAX ?= 3
K8S_STORM_DURATION ?= 90s
k8s-storm:
	@test "$(MODE)" = k8s || { echo "приложения не в кластере: make k8s-deploy"; exit 1; }
	@$(KUBECTL) patch hpa soldout --type merge -p '{"spec":{"maxReplicas":$(SOLDOUT_MAX)}}' >/dev/null
	@$(MAKE) pg-reset >/dev/null 2>&1 || true
	@$(MAKE) --no-print-directory holds-reset
	@mkdir -p $(RESULTS); tools/demo/k8s-timeline.sh 150 > $(RESULTS)/k8s-storm-timeline.txt & tl=$$!; \
	STORM_DURATION=$(K8S_STORM_DURATION) $(K6_RUN) --summary-export=/results/k8s-storm-$(STEP).json /scripts/storm.js; rc=$$?; \
	kill $$tl 2>/dev/null; cat $(RESULTS)/k8s-storm-timeline.txt; \
	$(KUBECTL) patch hpa soldout --type merge -p '{"spec":{"maxReplicas":3}}' >/dev/null; \
	$(KUBECTL) get hpa soldout; exit $$rc

# ---------- занятие 5: агент и кластер ----------
# kubeconfig агента: ServiceAccount agent-ro, только get/list/watch, без секретов. Агент диагностирует, но не меняет.
kubeconfig-ro:
	@KCTX=$(KCTX) tools/k8s/kubeconfig-ro.sh

# слом: лимит памяти soldout 96 МиБ при GOMEMLIMIT 440 МиБ. Без нагрузки под живет (десятки МиБ), короткий шторм
# поднимает кучу выше лимита раньше, чем GC начнет ее сдерживать: OOMKilled, затем CrashLoopBackOff.
break-oom:
	@$(HELM) upgrade soldout $(CHART) $(HELM_VALUES) --set soldout.resources.limits.memory=96Mi --set soldout.resources.requests.memory=96Mi --wait --timeout 2m 2> >(grep -v warnings.go >&2) >/dev/null
	@echo "выкачен soldout с limits.memory=96Mi, без нагрузки под Running; даем шторм на 15 с…"
	@(STORM_DURATION=15s $(K6_RUN) /scripts/storm.js >/dev/null 2>&1 || true) & \
	for i in 1 2 3 4 5 6; do sleep 7; $(KUBECTL) get pods -l app.kubernetes.io/name=soldout --no-headers; done; wait
	@echo "диагностика только на чтение: kubectl --kubeconfig .kube/agent-ro describe pod -l app.kubernetes.io/name=soldout; починка: make restore-k8s"

restore-k8s:
	@$(HELM) upgrade soldout $(CHART) $(HELM_VALUES) --wait --timeout 3m 2> >(grep -v warnings.go >&2) >/dev/null
	@$(KUBECTL) rollout status deploy/soldout --timeout 120s
	@$(KUBECTL) get pods

# fitness functions инфраструктуры: схема манифестов (kubeconform) и политики (conftest, deploy/policies/*.rego).
# Оба инструмента в контейнерах; на хосте нужен только helm. Схема ScaledObject берется из каталога CRD (нужна сеть).
KUBECONFORM := docker run --rm -i ghcr.io/yannh/kubeconform:v0.8.0
CONFTEST := docker run --rm -i -v $(abspath $(DEPLOY)/policies):/policy:ro openpolicyagent/conftest:v0.71.0
CRD_SCHEMAS := https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json
lint-k8s:
	@command -v helm >/dev/null || { echo "lint-k8s: нужен helm (brew install helm)"; exit 1; }
	@mkdir -p tmp && helm template soldout $(CHART) -f $(CHART)/values-k3d.yaml > tmp/k8s-rendered.yaml
	@$(KUBECONFORM) -strict -summary -schema-location default -schema-location '$(CRD_SCHEMAS)' - < tmp/k8s-rendered.yaml | sed 's/^/kubeconform: /'; test $${PIPESTATUS[0]} -eq 0
	@$(CONFTEST) verify --no-color -p /policy | tail -1 | sed 's/^/политики (conftest verify): /'; test $${PIPESTATUS[0]} -eq 0
	@$(CONFTEST) test --combine --no-color -p /policy - < tmp/k8s-rendered.yaml
	@echo "lint-k8s: ok"

fitness-red-k8s:   ## красный пример: копия чарта с latest и без readiness, политики находят ровно два нарушения
	@tools/k8s/fitness-red.sh

k8s-undeploy:
	-$(HELM) uninstall soldout --wait

k8s-down:
	-k3d cluster delete $(K3D_CLUSTER)
	@rm -f tmp/stand-mode

k8s-status:
	@$(KUBECTL) get deploy,hpa,scaledobject,pdb,pods,ingress

k8s-watch:         ## HPA, ScaledObject и поды раз в 2 с (Ctrl+C)
	@while true; do out=$$($(KUBECTL) get hpa,scaledobject,pods 2>&1); clear; date +%T; echo "$$out"; sleep 2; done

# ---------- нагрузка ----------
smoke:
	$(K6_RUN) --summary-export=/results/smoke-$(STEP).json /scripts/smoke.js

storm:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
	$(K6_RUN) --summary-export=/results/storm-$(STEP).json /scripts/storm.js

storm-short:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
	STORM_DURATION=60s $(K6_RUN) --summary-export=/results/storm-short-$(STEP).json /scripts/storm.js

# ---------- занятие 4: полный путь покупки ----------
# WITH_STORM=1 — параллельно вторым контейнером k6 идёт storm (мероприятие 1) с частотой BG_STORM_RATE итераций/с:
# конкуренция за пул. Фон намеренно ограничен: полный шторм упирает PostgreSQL в его 1 CPU, и акт 1 снова про базу.
BG_STORM_RATE ?= 400
checkout:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
ifeq ($(WITH_STORM),1)
	@mkdir -p $(RESULTS)
	@echo "фоновый шторм: $(BG_STORM_RATE) итераций/с на мероприятии 1, лог — $(RESULTS)/storm-bg-$(STEP).log"
	@STORM_DURATION=100s $(COMPOSE) --profile load run --rm --no-deps -e STORM_RATE=$(BG_STORM_RATE) -e STORM_VUS=300 k6 run $(K6_OUT) --summary-export=/results/storm-bg-$(STEP).json /scripts/storm.js > $(RESULTS)/storm-bg-$(STEP).log 2>&1 & \
	sleep 5; \
	$(K6_RUN_CHECKOUT) --summary-export=/results/checkout-storm-$(STEP).json /scripts/checkout.js; rc=$$?; \
	wait; exit $$rc
else
	$(K6_RUN_CHECKOUT) --summary-export=/results/checkout-$(STEP).json /scripts/checkout.js
endif

checkout-storm:
	$(MAKE) checkout WITH_STORM=1

pay-demo:
	@BASE_URL=$(HOST_URL) tools/demo/pay-demo.sh

# ---------- занятие 4: трейсы (Tempo) ----------
trace-last:        ## trace_id последнего pay из логов и ссылка на Grafana Explore
	@COMPOSE="$(COMPOSE)" $(if $(filter k8s,$(MODE)),SOLDOUT_LOGS="$(KUBECTL) logs -l app.kubernetes.io/name=soldout --tail=-1") tools/demo/trace.sh last

traces-slow:       ## TraceQL: pay дольше 1 с за 5 минут (свой запрос: make traces-slow Q='{ span.retry.attempt = 1 }')
	@COMPOSE="$(COMPOSE)" tools/demo/trace.sh slow $(if $(Q),'$(Q)',)

trace-json:        ## трейс в файл: make trace-json TRACE=<trace_id> (фолбэк для агента без Tempo MCP)
	@tools/demo/trace.sh json $(TRACE)

# ---------- занятие 4: события (outbox → Debezium → Kafka → notifier) ----------
KAFKA_EXEC := $(COMPOSE) exec -T kafka /opt/kafka/bin

cdc-status:        ## слот репликации (active, отставание WAL) и статус коннектора; пустая outbox — норма
	@$(PSQL) -c "SELECT slot_name, active, confirmed_flush_lsn, pg_size_pretty(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)) AS wal_behind FROM pg_replication_slots;" \
		-c "SELECT count(*) AS outbox_rows FROM outbox;"
	@curl -s localhost:8083/connectors/soldout-outbox/status | python3 -c 'import json,sys; d=json.load(sys.stdin); print("connector:", d["connector"]["state"], "tasks:", [t["state"] for t in d["tasks"]])'
	@$(KAFKA_EXEC)/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group notifier 2>/dev/null | awk 'NR==1 || /outbox/'

cdc-register:      ## (пере)регистрировать коннектор вручную; make up делает это сам
	@COMPOSE="$(COMPOSE)" tools/cdc/register.sh

# слом: notifier остановлен на NOTIFIER_DOWN секунд посреди checkout — лаг растёт, после старта догоняет без потерь
# и дублей. Останавливаются и стартуют все реплики (make scale NOTIFIER=3). Лаг отдаёт экспортёр notifier-lag (8091).
# NOTIFIER_DOWN=75 — старт уже после конца нагрузки: чистое время разбора очереди (замеры занятия 5).
NOTIFIER_DOWN ?= 40
LAG := curl -s localhost:8091/metrics | awk '/^notifier_lag /{print $$2}'
kill-notifier:
	@mkdir -p $(RESULTS); rm -f $(RESULTS)/.notifier-start; \
	(sleep 20; echo ">>> stop notifier"; $(COMPOSE) stop notifier >/dev/null; sleep $(NOTIFIER_DOWN); \
	 echo ">>> start notifier, лаг $$($(LAG))"; $(COMPOSE) start notifier >/dev/null; date +%s > $(RESULTS)/.notifier-start) & \
	$(K6_RUN_CHECKOUT) --summary-export=/results/checkout-kill-notifier-$(STEP).json /scripts/checkout.js; rc=$$?; wait; \
	echo "ждём, пока notifier догонит…"; \
	until [ "$$($(LAG))" = "0" ]; do sleep 1; done; \
	echo "лаг 0 через $$(( $$(date +%s) - $$(cat $(RESULTS)/.notifier-start) )) с после старта notifier"; sleep 6; \
	$(PSQL) -c "SELECT (SELECT count(*) FROM orders WHERE status = 'paid') AS paid, (SELECT count(*) FROM notifications WHERE kind = 'tickets_issued') AS notifications, (SELECT count(*) FROM (SELECT payload->>'order_id' FROM notifications WHERE kind = 'tickets_issued' GROUP BY 1 HAVING count(*) > 1) d) AS duplicates, (SELECT count(*) FROM notifications WHERE order_id IS NOT NULL AND status <> 'sent') AS notifier_not_sent;"; \
	exit $$rc

# слом: сообщение с невалидным payload — после NOTIFIER_MAX_ATTEMPTS попыток (~8 с) уходит в DLQ, поток идёт дальше
poison:
	@echo 'poison|{"order_id": "не-uuid", "oops": true}' | $(KAFKA_EXEC)/kafka-console-producer.sh --bootstrap-server localhost:9092 \
		--topic outbox.event.order --reader-property parse.key=true --reader-property key.separator='|' >/dev/null 2>&1
	@echo "в outbox.event.order отправлено сообщение с ключом poison; смотрим notifier (≈ 8 с)…"
	@sleep 10; $(COMPOSE) logs --no-log-prefix --since 20s notifier 2>/dev/null | grep -E "poison|DLQ|попытка" | tail -6
	@$(MAKE) --no-print-directory dlq

dlq:               ## содержимое DLQ с заголовками (error, attempts, original_offset)
	@$(KAFKA_EXEC)/kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic outbox.event.order.dlq --from-beginning \
		--timeout-ms 4000 --formatter-property print.headers=true --formatter-property print.key=true 2>/dev/null || true

# сверка I5: оплаченные заказы без письма (след break-goroutine на ветке otel) → OrderPaid в outbox → notifier
repair-i5:
	@$(PSQL) -q -f - < tools/cdc/repair-i5.sql
	@echo "ждём доставку (Debezium → Kafka → notifier)…"; sleep 8
	@$(MAKE) --no-print-directory invariants

# вариант A ADR-003 на полном стенде: notifier читает outbox polling'ом, Debezium не нужен (строки не удаляются в pay)
up-poll:
	NOTIFIER_SOURCE=outbox-poll OUTBOX_DELETE_AFTER_INSERT=false $(MAKE) up-stack

# Grafana service account (Viewer) и токен для mcp-grafana → .env.local (в .gitignore); идемпотентно
grafana-token:
	@tools/grafana/sa-token.sh

hot-row:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
	$(K6_RUN) --summary-export=/results/hot-row-$(STEP).json /scripts/hot-row.js

waiting-room:
	$(K6_RUN) --summary-export=/results/waiting-room-$(STEP).json /scripts/waiting-room.js

# 500 конкурентов разбирают 20 свободных мест сектора через POST /v1/holds/any (шаг C)
holds-any-demo:
	HOT_MODE=any $(K6_RUN) --summary-export=/results/holds-any-$(STEP).json /scripts/hot-row.js

queue-status:
	@curl -s $(HOST_URL)/v1/events/10000000-0000-4000-8000-000000000001/queue/stats | python3 -m json.tool

# ---------- проверки ----------
invariants:
	$(COMPOSE) --profile tools run --rm invariants

invariants-local:
	DATABASE_URL=postgres://soldout:soldout@localhost:5432/soldout?sslmode=disable go run ./tools/invariant-checker

lint-arch-install:
	go install github.com/fe3dback/go-arch-lint@v1.18.0

lint-arch:
	@test -x $(ARCH_LINT) || $(MAKE) lint-arch-install
	$(ARCH_LINT) check --output-color=false

# golang-migrate с x-multi-statement=true режет файл по каждой ';', в том числе
# внутри комментариев и строковых литералов, поэтому ';' там запрещена
lint-migrations:
	@! grep -nE -- "--.*;|'[^']*;[^']*'" migrations/*.sql || \
		(echo "lint-migrations: ';' в комментарии или строковом литерале миграции"; exit 1)
	@echo "lint-migrations: ok"

# fitness function занятия 4: сетевой вызов после commit в booking/app запрещён (semgrep в контейнере)
SEMGREP := docker run --rm -v $(CURDIR):/src -w /src semgrep/semgrep:latest semgrep --metrics=off --quiet
lint-semgrep:
	@out=$$($(SEMGREP) --config tools/semgrep/rules.yml --error --json internal/booking/app) || { echo "$$out" | python3 -c 'import json,sys; [print(r["path"], r["start"]["line"], r["extra"]["message"]) for r in json.load(sys.stdin)["results"]]'; exit 1; }; \
	n=$$(echo "$$out" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["paths"]["scanned"]))'); \
	test "$$n" -gt 0 || { echo "lint-semgrep: 0 файлов просканировано — проверка пустая"; exit 1; }; \
	echo "lint-semgrep: ok ($$n файлов)"

# контракт событий: @asyncapi/cli в контейнере
lint-asyncapi:
	@docker run --rm -e ASYNCAPI_METRICS=false -v $(CURDIR)/docs:/docs asyncapi/cli:latest validate /docs/asyncapi.yaml 2>&1 | grep -E "valid|error" 

fitness-red-semgrep:  ## красный пример: правило находит письмо и горутину после InTx
	-@$(SEMGREP) --config tools/semgrep/rules.yml --error tools/semgrep/red

lint-all: vet lint-arch lint-migrations lint-semgrep lint-k8s

test:
	go test ./... -count=1

test-unit:
	go test -short ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -l -w .

results:
	@python3 tools/k6-results.py $(RESULTS)

# ---------- профилирование ----------
# Go допускает один CPU-профиль одновременно: Alloy (pull в Pyroscope) ставится на паузу, и ждём конец его
# текущего 14-секундного профиля. Без ожидания — «cpu profiling already in use». Итого ≈ 40 с.
pprof-top:
	@mkdir -p $(RESULTS)
	-@$(COMPOSE_OBS) pause alloy >/dev/null 2>&1
	@for i in 1 2 3 4; do \
		if go tool pprof -top -nodecount=30 -seconds 20 $(HOST_URL)/debug/pprof/profile 2>/dev/null | tee $(RESULTS)/pprof-top-$(STEP).txt | grep -q flat; then break; fi; \
		echo "профиль занят Alloy, ждём 5 с…"; sleep 5; \
	done
	-@$(COMPOSE_OBS) unpause alloy >/dev/null 2>&1

# Профиль за прошедшее окно из Pyroscope (например, за шторм): make pprof-window FROM='now-5m' UNTIL='now'
pprof-window:
	@mkdir -p $(RESULTS)
	@curl -sG 'http://localhost:4040/pyroscope/render' --data-urlencode 'query=process_cpu:cpu:nanoseconds:cpu:nanoseconds{service_name="soldout"}' \
		--data-urlencode 'from=$(FROM)' --data-urlencode 'until=$(UNTIL)' -o $(RESULTS)/pyroscope-$(STEP).json
	@python3 tools/pyroscope-top.py $(RESULTS)/pyroscope-$(STEP).json 30 | tee $(RESULTS)/pprof-top-$(STEP).txt

pprof-heap:
	go tool pprof -top -nodecount=30 -sample_index=inuse_space $(HOST_URL)/debug/pprof/heap 2>/dev/null | head -40

pg-reset:
	@$(PSQL) -qc "SELECT pg_stat_statements_reset();" >/dev/null

pg-top:
	@$(PSQL) -c "SELECT calls, round(total_exec_time::numeric,1) AS total_ms, round(mean_exec_time::numeric,2) AS mean_ms, rows, round((shared_blks_hit::numeric / nullif(shared_blks_hit + shared_blks_read, 0)) * 100, 1) AS hit_pct, left(regexp_replace(query, '\s+', ' ', 'g'), 100) AS query FROM pg_stat_statements WHERE query NOT LIKE '%pg_stat_statements%' AND query NOT LIKE '%pg_stat_activity%' ORDER BY total_exec_time DESC LIMIT 10;"

pg-activity:
	@$(PSQL) -c "SELECT state, wait_event_type, wait_event, count(*) FROM pg_stat_activity WHERE datname = 'soldout' AND pid <> pg_backend_pid() GROUP BY 1,2,3 ORDER BY 4 DESC;"

explain-seatstates:
	@$(PSQL) -c "EXPLAIN (ANALYZE, BUFFERS) SELECT seat_id, status FROM holds WHERE event_id = '10000000-0000-4000-8000-000000000001' AND status IN ('active','confirmed');"

docker-stats:
	@docker stats --no-stream --format 'table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}'

pgbouncer-pools:
	@$(COMPOSE) exec -T -e PGPASSWORD=soldout pgbouncer psql -h 127.0.0.1 -p 6432 -U soldout pgbouncer -c "SHOW POOLS;" -c "SHOW STATS;"

# ---------- сломы живьём (каждый — одной командой; make restore возвращает стенд) ----------
break-cache:   ## слом №1: карта врёт — hold 409, инварианты зелёные
	CACHE_INVALIDATE=false $(MAKE) up-stack

break-admission: ## слом №2: admission rate 5000 — полка снова пик
	ADMISSION_RATE=5000 $(MAKE) up-stack

break-pool:    ## слом №3: пул 200 при max_connections 100 — всё краснеет обратно
	DATABASE_URL=postgres://soldout:soldout@postgres:5432/soldout?sslmode=disable DB_MAX_CONNS=200 DB_MAX_CONN_IDLE=30s $(MAKE) up-stack

break-prepared: ## ловушка PgBouncer: max_prepared_statements=0 (только для скриншота)
	PGBOUNCER_INI=pgbouncer-noprepared.ini $(MAKE) up-stack

# слом акта 3 живёт на demo/l4-otel: здесь письма уходят из outbox, режима горутины нет
break-goroutine:
	@echo "break-goroutine показывается на demo/l4-otel (до switch). Здесь то же падение после commit письмо не теряет:"
	@echo "  FAULT_CRASH_AFTER_COMMIT=order.paid make restore && make pay-demo && make invariants"

restore:
	$(MAKE) up-stack
