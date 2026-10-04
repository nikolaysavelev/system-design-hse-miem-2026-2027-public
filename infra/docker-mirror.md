# Docker Hub из РФ: зеркала

Docker Hub периодически недоступен из РФ. Все образы курса имеют явный тег и digest (`infra/images.lock`), поэтому подойдёт любое зеркало.

## Вариант 1. Зеркало в настройках Docker

Docker Desktop: Settings → Docker Engine, добавьте в JSON:

```json
{
  "registry-mirrors": [
    "https://mirror.gcr.io",
    "https://dockerhub.timeweb.cloud",
    "https://cr.yandex/mirror"
  ]
}
```

Linux: то же в `/etc/docker/daemon.json`, затем `sudo systemctl restart docker`. OrbStack: Settings → Docker → Registry mirrors.

Зеркала отдают только образы с Docker Hub. Образ `gcr.io/distroless/static-debian12` тянется напрямую с `gcr.io` (обычно доступен); если нет — замените в `soldout/Dockerfile` на `alpine:3.22` (см. комментарий в Dockerfile).

## Вариант 2. Скачать заранее

Под VPN или из сети, где Hub доступен:

```bash
infra/pull-all.sh
infra/pin-images.sh --check   # сверка digest'ов с images.lock
```

Образы остаются в локальном кэше Docker; дальше сеть не нужна.

## Вариант 3. Экспорт/импорт образов

```bash
docker save $(grep -vE '^\s*(#|$)' infra/images.txt) | gzip > soldout-images.tgz
docker load < soldout-images.tgz
```

## Публичный реестр курса

Решение о публикации копий образов в Yandex Container Registry (`cr.yandex/<id>/soldout/*`) принимается по итогам первого занятия (открытый вопрос спецификации курса). Пока используйте варианты 1–3.
