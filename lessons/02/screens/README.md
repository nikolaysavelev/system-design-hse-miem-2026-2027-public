# Скриншоты занятия 2 (фолбэк актов)

Сняты headless Chrome с Grafana/Pyroscope во время прогонов 10–11.09.2026 (kiosk-режим, 1600×900).

| Файл | Акт | Что видно |
|---|---|---|
| `act3-stepB-pg.png` | 3 | панель `pg` под storm-short на B: TPS до 8k/с, соединений 25 из 100, блокировки, n_dead_tup |
| `act3-stepB-pyroscope.png` | 1/3 | flame graph Pyroscope под нагрузкой |
| `act4-stepB-hotrow-red.png`, `act4-stepB-hotrow-k6.png` | 4 | `soldout-red` и `k6` под hot-row на B (advisory lock) |
| `act4-stepC-hotrow-red.png` | 4 | то же на C (без lock) |
| `act5-stepC-storm-red.png` | 2–4 | `soldout-red` под storm-short на C: 0 ошибок, p99 hold ≈ 400 мс |
| `act5-stepD-queue.png` | 5 | панель `soldout-queue` под waiting-room: размер очереди, допуски/с, ровная полка TPS |
| `act5-stepD-break-admission.png` | 5 | слом №2: `ADMISSION_RATE=5000` |

`pgbouncer-prepared.png` (ловушка prepared statements) снимается командой `make break-prepared` перед занятием: после переключения приложение падает на первом запросе с `ERROR: prepared statement "stmtcache_…" does not exist` — вывод `make logs`.
