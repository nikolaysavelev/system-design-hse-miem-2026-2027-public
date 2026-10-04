# Окружение

## Минимальные требования

| Режим | RAM | Что нужно |
|---|---|---|
| full | ≥ 16 ГБ | Docker Desktop / OrbStack / Podman с Compose v2, 20 ГБ диска, `make` |
| lite | 8–16 ГБ | то же; стенды поднимаются с оверлеем `compose/lite.yml` (без observability, уменьшенный seed) |
| аналитический | любой | ничего: работаете с артефактами из `hw/hwN/artifacts/` |

Windows — только WSL2 + Docker Desktop (backend WSL2). Для ноутбуков на Apple Silicon рекомендуется OrbStack: меньше накладных расходов на память, быстрее volume'ы.

Go (1.27) и k6 на хосте **не обязательны**: сборка сервисов и стрельба выполняются в контейнерах. Они нужны, только если вы хотите запускать `go test` и `go-arch-lint` локально, а не в Docker/CI.

## Проверка

```bash
tools/doctor.sh
```

Скрипт проверяет: версии Docker и Compose, лимит памяти Docker, доступность Docker Hub и зеркал, `docker run hello-world`, наличие `make`, `go`, `k6` (последние два — опционально).

## Лимит памяти Docker

Docker Desktop: Settings → Resources → Memory. Рекомендация: 8 ГБ для занятий 1–2, 12–16 ГБ для занятий 4–8 (в lite-профиле достаточно 6–8 ГБ). OrbStack подстраивает лимит сам.

## Docker Hub из РФ

Если `docker pull` не работает, настройте зеркало: [infra/docker-mirror.md](../infra/docker-mirror.md). Все образы курса перечислены в `infra/images.txt`, digest'ы — в `infra/images.lock`; `infra/pull-all.sh` скачивает всё заранее (например, под VPN).

## Запуск стенда

```bash
cd soldout
make up          # занятие 2 (по умолчанию): PostgreSQL 17 + PgBouncer + Valkey + soldout + obs-профиль
make up-01       # стенд занятия 1 без лимитов и observability
make smoke       # k6 smoke в контейнере
make invariants  # инвариант-чекер
make down        # остановить и удалить volume'ы
```

Стенд занятия 2 с obs-профилем (Prometheus, Grafana, Pyroscope + Alloy, exporters) занимает ≈ 2 ГБ RAM в покое и до 4 ГБ под `make storm`; лимиты CPU/памяти контейнеров заданы в `compose/lesson-02.yml` намеренно — без них на M4 узкие места не видны.

Lite-профиль (8–16 ГБ): `make up-lite` — seed 3 × 4 000 мест, без Pyroscope и exporters, лимиты сохраняются.

### Профилирование без Pyroscope (lite)

То же самое, что flame graph в Grafana, но текстом: под нагрузкой (`make storm-short` в соседнем терминале) выполните

```bash
make pprof-top       # go tool pprof -top: 20 с CPU-профиля с http://localhost:8080/debug/pprof/profile
make pprof-heap      # топ по памяти (inuse_space)
make pg-top          # топ-10 запросов pg_stat_statements
make pg-activity     # кто чего ждёт: state, wait_event_type, wait_event
```

Интерактивный flame graph локально: `go tool pprof -http=:8081 http://localhost:8080/debug/pprof/profile?seconds=20` (нужен Go на хосте).

## CI для домашних заданий

По умолчанию — GitHub Actions (`.github/workflows/ci.yml` запускает `go test`, `go-arch-lint`, сборку образа). Если GitHub Actions недоступен, тот же pipeline воспроизводится в GitVerse CI или GitLab CI: используются только `make test`, `make lint-arch` и `docker build`.
