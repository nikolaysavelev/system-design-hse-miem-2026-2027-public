# soldout — команды стенда. По умолчанию — стенд занятия 2 (LESSON=02); занятие 1: make up-01 / LESSON=01.
SHELL := /bin/bash
LESSON ?= 02
COMPOSE_OVERLAYS ?= -f compose/lesson-$(LESSON).yml
PROFILES ?= --profile obs
ifeq ($(LESSON),01)
PROFILES :=
endif
COMPOSE := docker compose -f docker-compose.yml $(COMPOSE_OVERLAYS)
COMPOSE_OBS := $(COMPOSE) $(PROFILES)
GOBIN ?= $(shell go env GOPATH)/bin
ARCH_LINT := $(GOBIN)/go-arch-lint
K6_OUT := $(if $(filter 02,$(LESSON)),-o experimental-prometheus-rw,)
# --no-deps: k6 не трогает стенд. Без него compose пересоздаёт soldout с дефолтным окружением
# и молча отменяет make break-* (стрельба идёт уже не по той конфигурации).
K6_RUN := $(COMPOSE) --profile load run --rm --no-deps k6 run $(K6_OUT)
PSQL := $(COMPOSE) exec -T postgres psql -U soldout -d soldout
RESULTS := k6/results
STEP ?= $(shell git rev-parse --abbrev-ref HEAD | tr '/' '-')

.PHONY: help build up up-01 up-02 up-lite up-obs down logs ps seed psql switch \
        smoke storm storm-short hot-row waiting-room holds-any-demo queue-status \
        invariants invariants-local lint-arch lint-arch-install lint-migrations lint-all test test-unit vet fmt \
        pprof-top pprof-window pprof-heap pg-top pg-reset pg-activity explain-seatstates results docker-stats pgbouncer-pools \
        break-cache break-admission break-pool break-prepared restore

help:
	@echo "Стенд:      up (=up-02) | up-01 | up-lite | down | switch BRANCH=demo/l2-stepA | logs | ps | psql"
	@echo "Нагрузка:   smoke | storm (2 мин) | storm-short (60 с) | hot-row | waiting-room | holds-any-demo"
	@echo "Проверки:   invariants | lint-arch | lint-migrations | lint-all | test | test-unit | results"
	@echo "Профиль:    pprof-top | pprof-heap | pg-top | pg-reset | pg-activity | explain-seatstates | docker-stats"
	@echo "Сломы:      break-cache | break-admission | break-pool | break-prepared | restore"

build:
	$(COMPOSE) build soldout postgres

up: up-$(LESSON)

up-01:
	$(MAKE) up-stack LESSON=01

up-02:
	$(MAKE) up-stack LESSON=02

up-stack:
	$(COMPOSE_OBS) up -d --build --wait
	@echo "готово: http://localhost:8080/readyz  Grafana: http://localhost:3000/d/soldout-red  Pyroscope: http://localhost:4040"

up-lite:
	$(MAKE) up-stack COMPOSE_OVERLAYS="-f compose/lesson-02.yml -f compose/lite.yml" PROFILES=

up-obs: up-02

down:
	$(COMPOSE) --profile load --profile tools --profile obs down -v --remove-orphans

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
	$(COMPOSE_OBS) build soldout
	$(COMPOSE_OBS) up -d --wait soldout
	@curl -s localhost:8080/readyz && echo

logs:
	$(COMPOSE) logs -f soldout

ps:
	$(COMPOSE_OBS) ps

psql:
	$(COMPOSE) exec postgres psql -U soldout -d soldout

seed:
	$(COMPOSE) exec -T postgres psql -U soldout -d soldout -v ON_ERROR_STOP=1 \
		-c "SET soldout.seats_per_event = '$${SEED_SEATS_PER_EVENT:-40000}'" -f - < seed/seed.sql

# ---------- нагрузка ----------
smoke:
	$(K6_RUN) --summary-export=/results/smoke-$(STEP).json /scripts/smoke.js

storm:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
	$(K6_RUN) --summary-export=/results/storm-$(STEP).json /scripts/storm.js

storm-short:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
	STORM_DURATION=60s $(K6_RUN) --summary-export=/results/storm-short-$(STEP).json /scripts/storm.js

hot-row:
	$(MAKE) pg-reset >/dev/null 2>&1 || true
	$(K6_RUN) --summary-export=/results/hot-row-$(STEP).json /scripts/hot-row.js

waiting-room:
	$(K6_RUN) --summary-export=/results/waiting-room-$(STEP).json /scripts/waiting-room.js

# 500 конкурентов разбирают 20 свободных мест сектора через POST /v1/holds/any (шаг C)
holds-any-demo:
	HOT_MODE=any $(K6_RUN) --summary-export=/results/holds-any-$(STEP).json /scripts/hot-row.js

queue-status:
	@curl -s localhost:8080/v1/events/10000000-0000-4000-8000-000000000001/queue/stats | python3 -m json.tool

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

lint-all: vet lint-arch lint-migrations

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
		if go tool pprof -top -nodecount=30 -seconds 20 http://localhost:8080/debug/pprof/profile 2>/dev/null | tee $(RESULTS)/pprof-top-$(STEP).txt | grep -q flat; then break; fi; \
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
	go tool pprof -top -nodecount=30 -sample_index=inuse_space http://localhost:8080/debug/pprof/heap 2>/dev/null | head -40

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

restore:
	$(MAKE) up-stack
