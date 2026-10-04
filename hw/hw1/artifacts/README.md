# Артефакты прогона преподавателя (lesson-1, 04–05.09.2026) — для аналитического режима HW1

Стенд: MacBook Pro M4, Docker 12 CPU / 8 ГБ, `make up` с seed 3 × 40 000 мест, `compose/lesson-01.yml`.

| Файл | Что это |
|---|---|
| `k6-smoke-summary.json` | `make smoke`: 10 VU × 60 с — зелёный (p99 hold 8.6 мс, seatmap ~90 мс, 0 % ошибок) |
| `k6-storm-summary.json` | `make storm`: 0→1000 VU за 10 с, 2 мин — красный: p99 hold 1.44 с, seatmap 2.1 с, 54.8 % ошибок («too many clients») |
| `k6-hot-row-summary.json` | `make hot-row`: 500 VU на 20 мест — красный: p99 hold 1.26 с, 48 % ошибок |
| `storm-pprof-top.txt` | `go tool pprof -top` CPU-профиля soldout за 30 с в середине storm |
| `pg_stat_statements-after-storm.txt` | топ-5 запросов по суммарному времени после smoke + storm + hot-row |
| `explain-seat-states.txt` | `EXPLAIN (ANALYZE, BUFFERS)` запроса статусов мест (`booking.seat_states`) — seq scan по `holds` |

Метрики k6: `metrics["http_req_duration{name:hold}"].p(99)` и т. п. Вопросы для анализа: где узкое место (пул vs БД vs сериализация JSON), что говорит профиль о `encoding/json` и syscall, почему `seat_states` не использует `uniq_active_hold`, какие два исправления дадут наибольший эффект.
