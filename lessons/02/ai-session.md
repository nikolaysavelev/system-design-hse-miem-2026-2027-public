# ИИ-нить занятия 2: агент + Postgres MCP (сохранённый диалог)

Фолбэк на случай, если на занятии нет VPN/MCP. Запись сделана 11.09.2026 на стенде `compose/lesson-02.yml`, состояние `demo/l2-stepB` (индексы шага A есть, advisory lock ещё на месте, кэш карты включён), сразу после `make storm-short` и `make hot-row`. Агент — Claude Code с сервером `postgres` из `.mcp.json` (`postgres-mcp --access-mode=restricted`: только чтение и EXPLAIN). Ответы инструментов приведены дословно (сокращены), ответы агента — как были даны.

Подключение: в `soldout/` → `claude` → `/mcp` показывает `postgres: connected`. Инструменты сервера: `get_top_queries`, `explain_query` (с гипотетическими индексами через hypopg), `analyze_workload_indexes`, `analyze_query_indexes`, `analyze_db_health`, `execute_sql` (read-only), `list_objects`, `get_object_details`.

---

## Промпт 1. «Проанализируй топ pg_stat_statements за последние минуты и предложи индексы с обоснованием»

**Инструмент `get_top_queries(sort_by=total_time, limit=8)`:**

| calls | total | mean | запрос |
|---|---|---|---|
| 114 424 | **147.3 с** | 1.29 мс | `/* booking.lock_seat */ SELECT pg_advisory_xact_lock(hashtext($1::text), $2::int)` |
| 114 424 | 2.6 с | 0.02 мс | `/* queue.insert_admission */ INSERT INTO admissions …` |
| 114 424 | 2.4 с | 0.02 мс | `/* catalog.get_seat */ SELECT … FROM seats s JOIN sectors …` |
| 114 424 | 1.3 с | 0.01 мс | `/* booking.count_user_holds */ SELECT count(*) FROM holds WHERE event_id = $1 AND user_id = $2 …` |
| 114 424 | 1.0 с | 0.01 мс | `/* booking.seat_sold */ SELECT EXISTS (… FROM holds WHERE event_id = $1 AND seat_id = $2 …)` |
| 228 848 | 0.8 с | 0.00 мс | `/* catalog.get_event */ SELECT … FROM events WHERE id = $1` |
| 114 425 | 0.6 с | 0.01 мс | `SELECT 1 FROM ONLY "public"."events" x WHERE "id" = $1 FOR KEY SHARE OF x` (FK-проверка) |

**Инструмент `analyze_workload_indexes(method=dta)`:** `No index recommendations found.`

**Ответ агента.** Топ по суммарному времени — не запрос к данным, а ожидание блокировки: `pg_advisory_xact_lock` — 147 с из ~156 с суммарно, при среднем 1.3 мс на вызов. Это очередь конкурентов на «горячих» местах (сценарий hot-row: 500 VU на 20 мест): сама транзакция короткая (count_user_holds и seat_sold — по 0.01 мс, они уже на индексах `holds_user_active_idx` и `holds_taken_uidx`), но каждый ждёт предыдущего. Индексов тут не хватает **ни одного**: DTA-анализ рабочей нагрузки (hypopg) не нашёл, что предложить. Рекомендация — не индекс, а убрать блокировку: уникальный partial-индекс `holds_taken_uidx (event_id, seat_id) WHERE status IN ('active','confirmed')` уже гарантирует «одно место — один hold», поэтому `INSERT … ON CONFLICT DO NOTHING RETURNING` даёт тот же инвариант без очереди: конфликт мгновенный, проигравший получает 409 за микросекунды. Побочное: FK-проверка `events FOR KEY SHARE` на каждом insert — заметна (114 k вызовов), но 0.6 с суммарно; трогать не стоит.

**Комментарий преподавателя.** Совпадает с шагом C. Обратите внимание: агент не предложил «индекс на advisory lock» — такого не бывает, и инструмент это подтвердил. На baseline (до шага A) тот же запрос показал бы `catalog.seats_by_venue` (576 с) и seq scan по `holds` — см. `pg-top-baseline.txt`: там агент предлагает `holds(event_id, user_id) WHERE status IN (…)` и `holds(expires_at) WHERE status='active'` (это и есть 0006–0007) и, как правило, лишний `holds(event_id, status)` — обсудить цену индекса на write path.

---

## Промпт 2. «Что показывает EXPLAIN для запроса статусов мест до и после? Поможет ли индекс (event_id, status)?»

**`explain_query` (запрос `booking.seat_states`):**

```
→ Seq Scan (Cost: 0.00..4459.00) on holds [Rows: 45304]
  Filter: ((status = ANY ('{active,confirmed}'::text[])) AND (event_id = '10000000-…-000000000001'::uuid))
```

**`explain_query` (лимит 4, `booking.count_user_holds`):**

```
→ Aggregate (Cost: 4.44..4.45) [Rows: 1]
  → Index Only Scan (Cost: 0.41..4.43) on holds [Rows: 1]      -- holds_user_active_idx
```

**`explain_query` с гипотетическим индексом `holds(event_id, status)` (hypopg):** план не изменился — `Seq Scan … [Rows: 45304]`.

**Ответ агента.** Статусы мест — это 45 тысяч строк из ~100 тысяч в `holds` (почти половина таблицы): планировщик правильно предпочитает Seq Scan, гипотетический индекс `(event_id, status)` он даже не выбрал. Индекс не поможет, потому что проблема не в поиске, а в объёме: запрос читает всю карту мероприятия на каждый вызов. Правильное исправление — не читать: проекция `catalog_seat_state` по секторам и кэш с версией (шаг B). Лимит 4 после шага A — Index Only Scan, 0.04 мс: индекс сработал там, где выборка узкая (одна пара event/user).

**Комментарий преподавателя.** Спецификация занятия так и говорила: `(event_id, status)` и `(seat_id) WHERE active` **не добавлять** — мёртвый груз на write path. Здесь это доказано инструментом, а не мнением.

---

## Промпт 3 (провокация). «Ускорь CreateHold, он медленный»

**Ответ агента (первая версия).** Три идеи: (1) убрать `pg_advisory_xact_lock` и положиться на уникальный индекс; (2) кэшировать «место занято» в Valkey и проверять кэш до похода в БД, чтобы 409 отдавать без транзакции; (3) проверять лимит 4 не в транзакции, а по счётчику в Valkey (`INCR user:{event}:{user}`), потому что `count_user_holds` всё равно выполняется на каждый hold.

**Что из этого нельзя и почему** (разбираем на занятии):

- (2) нарушает constitution 4 и I1: кэш — проекция, он «врёт про отображение и никогда про истину». Если кэш скажет «занято» про свободное место — потеряем продажу; если «свободно» про занятое — пойдём в БД, где индекс всё равно защитит. То есть кэш в пути hold либо бесполезен, либо опасен. Машинно это закрывает go-arch-lint: `booking_internal` не может импортировать `platform_cache` (`make fitness-red-arch`).
- (3) нарушает FR-7 как инвариант: счётчик в Valkey и строка в PostgreSQL — две истины без общей транзакции; при рестарте Valkey или гонке двух hold'ов пользователь получит 5 билетов, и I4 покраснеет. Лимит остаётся в транзакции по индексу `holds_user_active_idx` (0.04 мс — это не то, что нужно ускорять).
- (1) — верно и совпадает с шагом C.

**Ответ агента после возражения** («кэш в пути hold нарушает constitution — читай docs/constitution.md, раздел 4»): признаёт, оставляет только (1) и добавляет батчи expirer'а с `SKIP LOCKED`, чтобы фоновые UPDATE не блокировали hold'ы.

---

## Промпт 4. «Оцени здоровье БД»

**`analyze_db_health(health_type=all)`:**

```
Invalid index check: No invalid indexes found.
Duplicate index check: No duplicate indexes found.
Index bloat: No bloated indexes found.
Unused index check: Rarely used indexes found:
Index 'sectors_venue_id_idx' on table 'sectors' has only been scanned 0 times and uses 0.0MB of space
Index 'notifications_user_id_idx' on table 'notifications' has only been scanned 0 times and uses 0.0MB of space
Index 'order_items_hold_id_idx' on table 'order_items' has only been scanned 0 times and uses 0.0MB of space
Index 'payments_order_id_idx' on table 'payments' has only been scanned 0 times and uses 0.0MB of space
Connection health: Connections healthy: 28 total, 0 idle
Vacuum health: No tables found with transaction ID wraparound danger.
Sequence health: No sequences found in the database.
Replication health: This is a primary database.
No active replicas connected.
No replication slots found.
Buffer health for indexes: Index cache hit rate: 99.9% (above 95.0% threshold)
Buffer health for tables: Table cache hit rate: 99.9% (above 95.0% threshold)
Constraint health: No invalid constraints found.
=== 2. explain_query seat_states (без ANALYZE)
→ Seq Scan (Cost: 0.00..4459.00) on holds [Rows: 45304]
  Filter: ((status = ANY ('{active,confirmed}'::text[])) AND (event_id = '10000000-0000-4000-8000-000000000...
```

**Ответ агента.** База здорова: cache hit 99.9 %, соединений 28 при лимите 100 (пул 40 через PgBouncer с `default_pool_size` 20), invalid/дублирующих индексов нет. «Неиспользуемые» индексы — на таблицах, по которым ещё не было нагрузки (`payments`, `notifications`, `order_items`): удалять рано, это индексы под известные запросы occupancy-сценария оплаты, который в storm/hot-row не участвует. Стоит следить за `n_dead_tup` на `holds` (панель `pg`): HOT-update `active → released` выбивает строки из partial-индексов, autovacuum на 1 CPU отстаёт под штормом.

---

## Вывод для слайда

Агент — напарник по перформансу: он быстро читает `pg_stat_statements` и планы и правильно называет узкое место, когда оно в данных. Гипотезы проверяет измерение (EXPLAIN с hypopg, повторная стрельба), а границы задаёт constitution: два из трёх предложений по ускорению hold нарушали инварианты, и один из них ловит линтер. Restricted-режим MCP — обязательное условие: агент видит статистику, но не может ничего изменить.
