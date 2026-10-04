# AGENTS.md — инструкции для ИИ-агента в репозитории `soldout`

Ты работаешь в модульном монолите на Go 1.27. Прежде чем менять код, прочитай `docs/constitution.md` (правила), `spec/soldout.md` (что строим), `docs/adr/ADR-001-modular-monolith.md` (почему так) и, для HTTP, `api/openapi.yaml`.

## Где что лежит

- `cmd/soldout/main.go` — сборка зависимостей и HTTP-сервер; единственное место, где модули «знают» друг о друге целиком.
- `cmd/notifier` — сервис уведомлений (занятие 4, ADR-003): импортирует только `notification`, `platform`, `booking/api`. `cmd/mailgw` — эмулятор почтового шлюза.
- `internal/<module>/{api,domain,app,adapters}` — модули `catalog`, `queue`, `booking`, `payment`, `ticketing`, `notification`. Публично только `api`.
- `internal/platform/` — config, db (pgx, golang-migrate), cache (valkey-go), httpx (JSON, ошибки, middleware), log, metrics, faults, otel (трейсинг), outbox.
- `migrations/` — SQL по модулям; `seed/` — тестовые данные; `k6/` — сценарии; `tools/invariant-checker/` — инварианты.
- `../deploy/` — Helm-чарт `charts/soldout`, кластер k3d, политики conftest `policies/` (занятие 5, ADR-004); как устроено: `docs/deploy.md`.
- `.go-arch-lint.yml` — архитектурные правила; `Makefile` — все команды.

## Команды

```
make up          поднять стенд (Docker Compose), ждёт /readyz
make test        go test ./... (unit + интеграционный через testcontainers; нужен Docker)
make lint-arch   go-arch-lint check
make invariants  инвариант-чекер (на поднятом стенде)
make smoke       k6 smoke; make down — остановить
make lint-k8s    kubeconform + conftest по чарту (входит в make lint-all)
```

Локально без Docker: `go build ./... && go vet ./... && go test -short ./...`.

## Правила работы

1. **План до кода.** Для задач, затрагивающих более 3 файлов, сначала выведи список файлов (создать/изменить) и список тестов, дождись подтверждения, потом реализуй.
2. **Границы модулей.** Импортируй другой модуль только через `internal/<module>/api`. Нужны данные чужого модуля — добавь метод в его `api` и реализацию у владельца, не ходи в чужие таблицы.
3. **Миграции.** Не редактируй существующие файлы в `migrations/`; схема меняется только новым файлом `NNNN_name.up.sql` + `.down.sql`.
4. **Тесты обязательны.** Новая доменная операция → unit-тест в `domain`; новая конкурентная операция → интеграционный тест на testcontainers.
5. **Идемпотентность и таймауты** по умолчанию: см. constitution, разделы 4–5.
5a. **Событие, потеря которого нарушает инвариант, идет только через outbox** в транзакции издателя; после commit никаких сетевых вызовов, кроме тех, потерю которых система переживает.
5b. **Каждый запрос и каждое потребленное сообщение несут trace_id**, логи тоже; новый внешний вызов оборачивается в span (`otel.StartClient` или `otel.Transport`).
5c. **Манифесты и чарт (`../deploy/`) меняются только в git и проверяются `make lint-k8s`.** Каждый Deployment объявляет requests и limits, readiness, liveness, PDB, образ с фиксированным тегом.
5d. **В кластер ничего не применяй.** Никаких `kubectl apply`, `helm upgrade`, `kubectl edit`: выкатку делает пайплайн или человек. Для диагностики используй `kubectl --kubeconfig .kube/agent-ro` (только чтение).
6. **Готово = зелёные `make lint-arch`, `make test`** и (если стенд поднят) `make invariants`. Приведи вывод команд.
7. **Не трогай без просьбы**: `docs/constitution.md`, ADR, `api/openapi.yaml` (изменение контракта — отдельная задача с обновлением OpenAPI), `k6/`, `docker-compose.yml`.

## Коммиты

Сообщения на русском в формате `lesson-N: что сделано` (например, `lesson-1: модуль booking`). Без трейлеров, подписей и упоминания инструментов.

## Стиль

Комментарии и сообщения об ошибках — на русском; идентификаторы — на английском. Ошибки оборачивай с контекстом (`%w`). Логи — через `slog` из контекста запроса (`httpx.LoggerFrom`). Без глобальных переменных состояния.
