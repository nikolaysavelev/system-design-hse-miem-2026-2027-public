# Baseline занятия 2: `lesson-1` под лимитами `compose/lesson-02.yml`

Прогон 10.09.2026 23:15 (MacBook Pro M4, Docker Desktop 12 CPU / 8 ГБ). Ветка `demo/l2-baseline` = `lesson-1` + E.1 (лимиты, параметры PostgreSQL, obs-профиль, PgBouncer в compose) + E.2 (`GOMEMLIMIT`, таймауты сервера, ограниченное ожидание пула, батчи expirer). Пул приложения — **напрямую** в PostgreSQL, `DB_MAX_CONNS=200` при `max_connections=100` (как на занятии 1): `make break-pool`.

## Лимиты

| Контейнер | CPU | RAM | Примечание |
|---|---|---|---|
| postgres | 1.0 | 1 ГБ | `shared_buffers=256MB`, `work_mem=8MB`, `statement_timeout=5s`, `idle_in_transaction_session_timeout=5s` |
| soldout | 2.0 | 512 МБ | `GOMEMLIMIT=440MiB`, `GOGC=100`; `/readyz`: `gomaxprocs: 2`, `gomemlimit: 461373440` |
| valkey, pgbouncer | 0.5 | — | |
| k6 | без лимитов | — | |

## Результат: красный

| Сценарий | RPS | Ошибки | hold p50 / p95 / p99 | seatmap p99 | Порог |
|---|---|---|---|---|---|
| `make storm` (1 000 VU, 2 мин) | 426 | **22.9 %** (13 701 из 59 717, все — 503) | 2 782 / 3 002 / **3 007 мс** | 3 038 мс | ошибки < 1 %, p99 hold < 500 мс — **не пройден** |
| `make hot-row` (500 VU, 20 мест) | 592 | 0 % | 993 / 1 501 / **1 686 мс** | — | p99 < 200 мс — **не пройден** |
| `make invariants` после каждого | — | I1–I4 OK | | | зелёный |

Для сравнения — тот же код без лимитов (занятие 1): storm 2 020 RPS, p99 1 443 мс, 54.8 % ошибок (`too many clients`); hot-row 2 226 RPS, p99 1 269 мс, 48.4 % ошибок.

Критерий из спецификации («ошибки ≥ 40 % или p99 hold ≥ 1 с») выполнен по p99: 3 с — это `HANDLER_TIMEOUT`, запросы просто не успевают. Ужесточать лимиты не потребовалось.

## Причина «красного» — названа

1. **CPU, а не соединения.** `docker stats` в середине шторма: `postgres 105 %` (лимит 1 CPU), `soldout 98 %` (лимит 2 CPU), pgbouncer 26 %, остальное < 2 %. В логах приложения `too many clients` — 0 раз: пропускная способность упёрлась в CPU раньше, чем пул дорос до 100 соединений. Ошибки — 503 `timeout` (бюджет хендлера 3 с) и 503 `db_busy` (ожидание соединения > 2 с): `/v1/holds` 7 494, `/v1/events/{id}/seatmap` 5 786, `queue/join` 421.
2. **Карта зала целиком.** `pg-top`: `catalog.seats_by_venue` — 5 626 вызовов × 102 мс = **576 с** суммарного времени БД (225 млн строк отдано за 2 минуты); профиль CPU приложения (Pyroscope, `pprof-top-baseline.txt`): `SeatsByVenue` — 68 % cum, из них `pgx.Scan`/`uuid.Parse` ≈ 47 %, `encoding/json` ≈ 15 %. Каждый пятый запрос k6 тянет 1.8 МБ.
3. **Seq Scan по `holds`** для статусов мест (`explain-before.txt`): 15 149 строк, 368 буферов, 2 мс на пустой БД — под нагрузкой в очереди к одному ядру это десятки миллисекунд на каждый seatmap; `count_user_holds` (лимит 4) — 19 533 × 3 мс = 60 с, тоже seq scan.
4. **Горячий ряд: очередь на advisory lock.** hot-row без ошибок, но p50 = 993 мс: 500 конкурентов на 20 мест выстраиваются в очередь `pg_advisory_xact_lock` и держат соединение пула, пока ждут.
5. FK-проверки `holds → events/seats` (`FOR KEY SHARE`, 41 572 + 15 267 вызовов) — заметны, но не решают исход; остаются.

## Стенд с obs-профилем

`docker stats` в покое / под штормом: soldout 25 / 343 МБ, postgres 60 / 99 МБ (+ page cache), grafana 336 МБ, prometheus 90 МБ, pyroscope 43 МБ, alloy 40 МБ, k6 до 350 МБ. Итого ≈ 1.3 ГБ в покое, ≈ 1.9 ГБ под штормом — в ориентир ≤ 5 ГБ укладывается с запасом. `docker inspect soldout`: `OOMKilled=false`, рестартов 0 — `GOMEMLIMIT` держит.

`make up` (образы собраны): 18 с до `readyz` со всем obs-профилем; холодная сборка образа PostgreSQL с hypopg — ещё ≈ 2.5 мин один раз.

## Что дальше

Шаг A (индексы, пул 40 через PgBouncer): ожидаем уход 503 `db_busy` и ускорение `count_user_holds`/`seat_sold`, но seatmap целиком останется — p99 выше порога. Шаг B убирает пункт 2, шаг C — пункт 4.
