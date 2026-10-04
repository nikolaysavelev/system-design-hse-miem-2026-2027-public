# Занятие 2: демо по актам — команды, ожидаемые цифры, фолбэки

Все команды — из `soldout/` (`cd soldout`). Обозначения: `EVENT` = `10000000-0000-4000-8000-000000000001`, `SECTOR` = `30000000-0000-4000-8000-0000000003e9` (сектор 1). Ожидаемые цифры — диапазоны по прогону 11.09 (`results.md`); точные значения плавают ±20 %. Фолбэк любого шага — `results.md` и PNG в `screens/`.

## Подготовка (до занятия)

```bash
tools/doctor.sh
cd soldout && make down && make up          # стенд занятия 2 с obs-профилем; ≈ 20 с при собранных образах
curl -s localhost:8080/readyz | jq .        # gomaxprocs: 2, gomemlimit, pgbouncer: "1.25.2"
git switch demo/l2-baseline                 # стартовое состояние акта 1
make break-pool                             # = baseline: пул 200 напрямую в PostgreSQL (без PgBouncer)
```

Открыть: Grafana `http://localhost:3000/d/soldout-red` (и `d/pg`, `d/k6`), Pyroscope `http://localhost:4040`, вторая вкладка терминала для `make pg-top`/`pg-activity`. В Claude Code проверить `/mcp` → `postgres` подключён.

## Акт 1 (12–24 мин). Профиль под штормом

Терминал 1 — шторм фоном (2 мин 20 с):

```bash
make storm                                  # baseline: ожидаем ошибки ≈ 20–25 % (503 timeout), p99 hold ≈ 3 с, RPS ≈ 400–450
```

Терминал 2, пока идёт шторм (в этом порядке):

```bash
make docker-stats                           # postgres ≈ 100 % CPU (лимит 1), soldout ≈ 95–100 % (лимит 2)
make pg-activity                            # соединения: idle in transaction / active; ClientRead; под hot-row — advisory
make pg-top                                 # топ: catalog.seats_by_venue (40 000 строк × ~100 мс), count_user_holds, seat_sold
make explain-seatstates                     # Seq Scan on holds (explain-before.txt)
```

Grafana → Explore → Pyroscope → `process_cpu` / `soldout`: flame graph — `SeatsByVenue` (pgx.Scan + uuid.Parse) и `encoding/json` ≈ 70 % CPU. Текстовый аналог: `make pprof-window FROM=now-2m UNTIL=now`.

Что сказать пальцем: ошибок 20 % при p99 3 с — это `HANDLER_TIMEOUT`; «p99 при ошибках — ложь», сначала убираем ошибки.

**Фолбэк:** `lessons/02/baseline.md`, `pprof-top-baseline.txt`, `pg-top-baseline.txt`, `screens/act1-*.png`.

## Акт 2 (24–38 мин). Шаг A: индексы + пул + PgBouncer

```bash
make switch BRANCH=demo/l2-stepA            # git switch + пересборка + readyz; миграции 0006–0010 применяются при старте
git diff demo/l2-baseline..demo/l2-stepA --stat
cat migrations/0010_holds_taken_uidx.up.sql  # один CREATE INDEX CONCURRENTLY на файл — почему
make explain-seatstates                     # count_user_holds: Index Only Scan (explain-after.txt)
curl -s localhost:8080/readyz | jq .pgbouncer,.db_max_conns   # "PgBouncer 1.25.2", 40
make pgbouncer-pools                        # SHOW POOLS: cl_active ≤ 40, sv_active ≤ 20
make storm-short                            # 60 с: ошибки 55 % → ≈ 5 % (все — 503 карты зала), p99 hold 3.0 → ≈ 2.6 с, RPS ≈ 500
make invariants
```

Что сказать: пул не ускоряет — переносит очередь в приложение (`db_pool_acquire_wait_seconds` на дашборде); `default_pool_size` 20 < 40 — иначе пулер декорация; при одном инстансе он нужен «под N подов занятия 5». p99 остался высоким — карта зала целиком, это шаг B.

Ловушка PgBouncer + prepared statements — **по скриншоту** (`screens/pgbouncer-prepared.png`, 90 с): `make break-prepared` → `ERROR: prepared statement "stmtcache_…" does not exist`; фикс — `max_prepared_statements = 200` (PgBouncer ≥ 1.21) или `?default_query_exec_mode=exec` в `DATABASE_URL`. Три варианта совместимости: простой протокол, PgBouncer 1.21+ с `max_prepared_statements`, режим `exec` без кэша.

## Акт 3 (38–48 мин). Шаг B: карта по секторам, eventbus, версионированный кэш, ETag

```bash
make switch BRANCH=demo/l2-stepB
git diff demo/l2-stepA..demo/l2-stepB --stat  # platform/eventbus, catalog_seat_state, adapters/valkey, миграции 0011–0012
curl -s localhost:8080/v1/events/$EVENT/seatmap | jq '.sectors[0]'            # сводка: 5.8 КБ вместо 1.8 МБ
curl -sD - -o /dev/null localhost:8080/v1/events/$EVENT/sectors/$SECTOR/seatmap | grep -iE 'etag|x-cache'   # miss → hit
curl -s -o /dev/null -w '%{http_code}\n' -H 'If-None-Match: "<версия>"' localhost:8080/v1/events/$EVENT/sectors/$SECTOR/seatmap  # 304
make storm-short                            # ошибки → 0 %, RPS ≈ 4 200, p99 hold ≈ 400 мс, карта сектора p99 ≈ 4 мс (p50 0.3 мс)
make pg-top                                 # catalog.seats_by_venue исчез; sector_states — единицы мс
make invariants                             # I6 (проекция = holds) OK; сразу после шторма может быть лаг — повторить через 20 с
```

**Слом №1** (кэш врёт про отображение, не про истину):

```bash
make break-cache                            # CACHE_INVALIDATE=false
# hold места, которое карта показывает free → 201; та же карта → всё ещё free (X-Cache: hit); второй hold → 409 seat_held
make invariants                             # зелёный
make restore
```

Что сказать: проекция — собственная таблица catalog, чтение чужой таблицы `holds` исчезло из SQL; `platform/eventbus` — шов, на занятии 3 Publisher станет outbox; stale-while-revalidate — версия в ETag честная, это версия отданных данных.

## Акт 4 (48–60 мин). Горячий ряд — «ага»

На stepB (advisory lock ещё на месте):

```bash
make hot-row HOT_MODE=specific              # 500 VU на 20 мест: ошибок 0, p99 ≈ 235 мс — lock ждут, но транзакция короткая
make pg-activity                            # wait_event = advisory (снимок во время стрельбы, pg-activity-stepB-hotrow.txt)
make pg-top                                 # booking.lock_seat — первое место по total_ms
```

Шаг C:

```bash
make switch BRANCH=demo/l2-stepC
git diff demo/l2-stepB..demo/l2-stepC -- internal/booking/adapters/pg/store.go   # LockSeat удалён, INSERT … ON CONFLICT DO NOTHING
make hot-row HOT_MODE=specific              # p99 ≈ 200 мс, 0 таймаутов, 409 мгновенные; RPS 3.8k → 4.5k
make holds-any-demo                         # мероприятие 2: 500 конкурентов на сектор с 20 свободными → 19–20 × 201, остальное 409 sector_exhausted, 0 × 5xx, p99 ≈ 3 мс
make invariants
```

Что сказать: «advisory lock дублировал уникальный индекс и создавал очередь; меньше механизмов — быстрее и правильнее»; `/holds/any`: случайный старт распределяет кандидатов, гонку закрывает индекс; идемпотентность — в той же транзакции.

## Акт 5 (60–67 мин). Waiting room — по записи

`results.md`, раздел «Шаг D», панель `soldout-queue`. Если stepD собран: `make switch BRANCH=demo/l2-stepD && QUEUE_ENABLED=true make restore && make waiting-room`; слом №2: `make break-admission` (полка → пик) → `make restore`.

## ADR-002 + чек-лист + слом №3 (67–72 мин)

```bash
make break-pool                             # DATABASE_URL напрямую, DB_MAX_CONNS=200: всё краснеет обратно
make storm-short                            # ≈ baseline
make restore
```

`docs/adr/ADR-002-queue-state.md` — таблица CBAM-lite, решение C; `docs/perf-checklist.md` — «унесите с собой».

## ИИ-нить (72–77 мин)

В `soldout/` → `claude`, MCP `postgres` (restricted). Промпты и ожидания — `lessons/02/ai-session.md` (сохранённый диалог как фолбэк).

## После занятия

```bash
make down
```

## Замеры команд (прогон 11.09.2026)

См. `results.md`, раздел «Время команд demo.md».
