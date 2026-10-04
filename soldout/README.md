# soldout — сквозная система курса

Сервис продажи билетов на «дроп»: 40 000 мест, 2 000 000 желающих, старт продаж в 12:00. Модульный монолит на Go 1.27 с PostgreSQL 17 и Valkey 9. Тег `lesson-1` — исходное состояние; тег `lesson-2` — после оптимизации под лимитами (индексы, PgBouncer, карта зала по секторам с кэшем, `ON CONFLICT` вместо блокировок, waiting room с admission rate). Ветки `demo/l2-baseline`, `demo/l2-stepA…D` — состояние после каждого шага (`lessons/02/results.md`).

## Пять команд

```bash
make up          # стенд занятия 2: postgres + pgbouncer + valkey + soldout + obs (Prometheus, Grafana, Pyroscope); make up-01 — стенд занятия 1
make smoke       # k6: 10 VU × 60 с — seatmap → queue/join → hold → order → pay; должен быть зелёным
make invariants  # инвариант-чекер I1–I4 по данным PostgreSQL; должен быть зелёным
make lint-arch   # go-arch-lint: правило зависимостей модулей (fitness function)
make test        # go test ./... — unit-тесты домена + интеграционный (testcontainers, нужен Docker)
```

Дополнительно: `make storm` / `make storm-short` (1 000 VU: на `lesson-1` красный, на `lesson-2` зелёный), `make hot-row` (`HOT_MODE=specific|any`), `make holds-any-demo`, `make waiting-room` (`QUEUE_ENABLED=true`), `make results` (таблица k6), профилирование `make pprof-top | pprof-window | pg-top | pg-activity | explain-seatstates`, сломы `make break-cache | break-admission | break-pool | restore`, fitness functions `make lint-all` (`lint-arch`, `lint-sql`, `lint-semgrep`) и их красные примеры `make fitness-red-*`, `make up-lite`, `make down`.

## Что где

| Путь | Назначение |
|---|---|
| `cmd/soldout/main.go` | сборка зависимостей, HTTP-сервер, graceful shutdown |
| `internal/<module>/{api,domain,app,adapters}` | модули `catalog`, `queue`, `booking`, `payment`, `ticketing`, `notification` |
| `internal/platform/` | config, db (pgx + golang-migrate), cache (valkey-go), httpx, log, metrics, faults, outbox (пусто до L3) |
| `migrations/`, `seed/` | SQL-миграции (по модулям) и seed 3 × 40 000 мест; встроены в бинарник |
| `api/openapi.yaml` | контракт HTTP API |
| `spec/soldout.md` | спецификация системы (ФТ, НФТ, расчёт нагрузки, модули, инварианты) |
| `docs/constitution.md`, `AGENTS.md`, `CLAUDE.md` | правила для людей и агентов |
| `docs/adr/ADR-001-modular-monolith.md` | решение о стартовой архитектуре и известные ограничения MVP |
| `.go-arch-lint.yml`, `.semgrep.yml`, `tools/check-sql-owners.py`, `docs/table-owners.yml` | fitness functions: зависимости модулей, сессионные lock/`SELECT *`, владение таблицами |
| `internal/platform/eventbus` | in-process шина событий (шов для outbox на занятии 3) |
| `compose/lesson-02.yml`, `compose/pgbouncer`, `compose/postgres` (hypopg), `compose/alloy`, `compose/grafana` | стенд занятия 2 с лимитами и наблюдаемостью |
| `.mcp.json` (в корне репозитория) | Postgres MCP в restricted-режиме для ИИ-нити |
| `k6/` | `smoke`, `storm`, `hot-row`, `waiting-room` |
| `tools/invariant-checker/` | проверка инвариантов I1–I4 (I5 — с занятия 3) |
| `docker-compose.yml`, `compose/` | стенд; оверлеи `lesson-01.yml`, `lite.yml`; профили `load`, `tools`, `obs` |

## Правило зависимостей (кратко)

Модуль импортирует другой модуль только через его пакет `api`. Разрешённые рёбра: `booking → catalog.api, queue.api`; `payment → booking.api`; `ticketing → booking.api`; `notification → ticketing.api`; `queue → catalog.api`; `catalog → booking.api` (статусы мест и sold_count). `platform` не знает о бизнес-модулях. Проверяется `go-arch-lint`.

## Быстрая проверка руками

```bash
EVENT=10000000-0000-4000-8000-000000000001; USER=$(uuidgen | tr A-Z a-z)
TOKEN=$(curl -s -XPOST localhost:8080/v1/events/$EVENT/queue/join -d "{\"user_id\":\"$USER\"}" | jq -r .token)
HOLD=$(curl -s -XPOST localhost:8080/v1/holds -H "X-Admission-Token: $TOKEN" -d "{\"event_id\":\"$EVENT\",\"seat_id\":1,\"user_id\":\"$USER\"}" | jq -r .id)
ORDER=$(curl -s -XPOST localhost:8080/v1/orders -H "Idempotency-Key: $(uuidgen)" -d "{\"hold_ids\":[\"$HOLD\"],\"user_id\":\"$USER\"}" | jq -r .id)
curl -s -XPOST localhost:8080/v1/orders/$ORDER/pay | jq .status
curl -s localhost:8080/v1/users/$USER/tickets | jq .
```

## Известные ограничения (см. ADR-001, таблица статусов)

После занятия 2 остаются: выпуск билета и уведомление — синхронно внутри `pay` (занятие 3: outbox); проекция карты зала eventually consistent (in-process шина; при переполнении события отбрасываются, страховка — TTL кэша); нет компенсации платежа при истёкшем hold (занятие 7).
