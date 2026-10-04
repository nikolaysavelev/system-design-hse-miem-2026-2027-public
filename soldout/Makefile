# soldout — команды стенда занятия 1. Всё, кроме test/lint-arch, выполняется в Docker.
SHELL := /bin/bash
COMPOSE_OVERLAYS ?= -f compose/lesson-01.yml
COMPOSE := docker compose -f docker-compose.yml $(COMPOSE_OVERLAYS)
GOBIN ?= $(shell go env GOPATH)/bin
ARCH_LINT := $(GOBIN)/go-arch-lint
K6_RUN := $(COMPOSE) --profile load run --rm k6 run

.PHONY: help build up up-lite up-obs down logs ps seed psql \
        smoke storm hot-row waiting-room invariants invariants-local lint-arch lint-arch-install test test-unit vet fmt

help:
	@echo "make up          собрать и поднять стенд (postgres, valkey, soldout), ждать readyz"
	@echo "make up-lite     то же с оверлеем compose/lite.yml (seed 3×4000 мест)"
	@echo "make up-obs      + Prometheus (9090) и Grafana (3000)"
	@echo "make down        остановить и удалить volume'ы"
	@echo "make smoke       k6 smoke: 10 VU × 60 с (seatmap → hold → order → pay)"
	@echo "make storm       k6 storm: 0→1000 VU за 10 с, 2 мин — ожидаемо красный по p99 (занятие 2)"
	@echo "make hot-row     k6 hot-row: 500 VU на 20 мест первого ряда"
	@echo "make invariants  инвариант-чекер I1–I4"
	@echo "make lint-arch   go-arch-lint check (правило зависимостей модулей)"
	@echo "make test        go test ./... (unit + интеграционный через testcontainers)"
	@echo "make seed | psql | logs | ps"

build:
	$(COMPOSE) build soldout

up:
	$(COMPOSE) up -d --build --wait postgres valkey soldout
	@echo "готово: http://localhost:8080/readyz"

up-lite:
	$(MAKE) up COMPOSE_OVERLAYS="-f compose/lesson-01.yml -f compose/lite.yml"

up-obs:
	$(COMPOSE) --profile obs up -d --build --wait
	@echo "Prometheus: http://localhost:9090  Grafana: http://localhost:3000/d/soldout-red"

down:
	$(COMPOSE) --profile load --profile tools --profile obs down -v --remove-orphans

logs:
	$(COMPOSE) logs -f soldout

ps:
	$(COMPOSE) ps

psql:
	$(COMPOSE) exec postgres psql -U soldout -d soldout

# Повторно применить seed (только если таблица events пуста; иначе сначала make down).
seed:
	$(COMPOSE) exec -T postgres psql -U soldout -d soldout -v ON_ERROR_STOP=1 \
		-c "SET soldout.seats_per_event = '$${SEED_SEATS_PER_EVENT:-40000}'" -f - < seed/seed.sql

smoke:
	$(K6_RUN) --summary-export=/results/smoke-summary.json /scripts/smoke.js

storm:
	$(K6_RUN) --summary-export=/results/storm-summary.json /scripts/storm.js

hot-row:
	$(K6_RUN) --summary-export=/results/hot-row-summary.json /scripts/hot-row.js

waiting-room:
	$(K6_RUN) --summary-export=/results/waiting-room-summary.json /scripts/waiting-room.js

invariants:
	$(COMPOSE) --profile tools run --rm invariants

# Локальный запуск чекера (нужен Go на хосте): порт 5432 проброшен наружу.
invariants-local:
	DATABASE_URL=postgres://soldout:soldout@localhost:5432/soldout?sslmode=disable go run ./tools/invariant-checker

lint-arch-install:
	go install github.com/fe3dback/go-arch-lint@v1.18.0

lint-arch:
	@test -x $(ARCH_LINT) || $(MAKE) lint-arch-install
	$(ARCH_LINT) check --output-color=false

test:
	go test ./... -count=1

test-unit:
	go test -short ./... -count=1

vet:
	go vet ./...

fmt:
	gofmt -l -w .
