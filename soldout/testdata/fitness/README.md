# Fitness functions занятия 2 — «красные» примеры

Каждое правило имеет пример, на котором оно срабатывает. Примеры лежат вне сборки (`testdata/` игнорируется `go build`/`go vet`).

| Правило | Инструмент | Красный пример | Как показать |
|---|---|---|---|
| Кэш не читается в пути hold: `booking_internal` ↛ `platform_cache` | go-arch-lint | `red/booking_reads_cache.go.txt` | `make fitness-red-arch` (копирует файл в `internal/booking/app`, lint красный, убирает) |
| Сессионный advisory lock под пулером запрещён; `SELECT *`; `context.Background()` вне `cmd`/тестов; `time.Now()` в `*/domain` | semgrep (`.semgrep.yml`) | `red/semgrep_red.go` | `make fitness-red-semgrep` |
| SQL-литерал начинается с `/* module.name */`; модуль трогает только свои таблицы (`docs/table-owners.yml`) | `tools/check-sql-owners.py` | `red/sql_owners_red.go` | `make fitness-red-sql` |
