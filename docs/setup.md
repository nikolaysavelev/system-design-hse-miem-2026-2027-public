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

## Запуск стенда занятия 1

```bash
cd soldout
make up          # PostgreSQL 17 + Valkey 9 + soldout; миграции и seed применяются при старте
make smoke       # k6 smoke в контейнере
make invariants  # инвариант-чекер
make down        # остановить и удалить volume'ы
```

Lite-профиль: `make up COMPOSE_OVERLAYS="-f compose/lite.yml"` (или `make up-lite`).

## CI для домашних заданий

По умолчанию — GitHub Actions (`.github/workflows/ci.yml` запускает `go test`, `go-arch-lint`, сборку образа). Если GitHub Actions недоступен, тот же pipeline воспроизводится в GitVerse CI или GitLab CI: используются только `make test`, `make lint-arch` и `docker build`.
