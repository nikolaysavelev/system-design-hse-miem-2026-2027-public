# Занятие 1: демо по актам — команды и промпты

Все команды выполняются из корня репозитория, если не сказано иное. Терминал: крупный шрифт, `cd soldout` открыт во второй вкладке. Перед занятием: `make down && make up` (образ собран, стенд поднят). Замеры времени каждого шага — в конце файла.

## Подготовка (до занятия, ~2 мин)

```bash
tools/doctor.sh                       # окружение
cd soldout && make down && make up    # чистый стенд: postgres + valkey + soldout, ждёт /readyz
curl -s localhost:8080/readyz         # {"ready":true,...}
cd ..
```

## Акт 1 (42–48 мин): агент без спеки

Промпт (уже выполнен, результат сохранён):

```
Сделай сервис продажи билетов на концерт на Go с PostgreSQL. Нужны бронирование мест и оплата.
```

```bash
git switch demo/act1-naive
cat lessons/01/act1-naive/main.go     # читаем bookSeat и paySeat
cat lessons/01/act1-review.md         # таблица дефектов; разбираем №1, 2, 3, 4
git switch main
```

Что показать пальцем: `var db *sql.DB`; `SELECT * … WHERE seat_id` → `INSERT` без блокировки; нет `expires_at`; `/pay` вставляет платёж на каждый вызов.

## Акт 2 (48–64 мин): агент со спекой, живьём

```bash
git switch demo/act2-start
cd soldout
ls internal/booking                   # только api/ — контракт есть, реализации нет
ls migrations                         # нет 0002_booking
make down && make up                  # стенд без booking (маршрутов /v1/holds нет — 404)
claude                                # Claude Code в soldout/
```

Промпт агенту:

```
Прочитай AGENTS.md, docs/constitution.md, spec/soldout.md и ADR-001. Реализуй модуль booking (domain, app, adapters/pg, adapters/http) согласно спецификации: CreateHold, ReleaseHold, CreateOrder, PayOrder с истечением hold и лимитом 4 билета. Начни с плана: список файлов и тестов. После реализации прогони make lint-arch test.
```

Ожидаемая последовательность и комментарии преподавателя:

1. Агент выводит план файлов (правило AGENTS.md «план для задач > 3 файлов») — обращаем внимание, что в плане есть миграция `0002_booking.up.sql` и два вида тестов.
2. Код: `domain/hold.go` с `ExpiresAt`/лимитом (constitution 3, FR-3/FR-7); `adapters/pg` с транзакцией, блокировкой места и обработкой `uniq_active_hold` (constitution 4: «проверил — потом записал» запрещено); `adapters/http` с маппингом ошибок в 409/422.
3. `make lint-arch` зелёный — агент импортирует `catalog/api`, `queue/api`, а не `catalog/domain`.
4. `make test` — unit-тесты domain + интеграционный (testcontainers).

Проверка руками (после `make up` с новым кодом):

```bash
make up
EVENT=10000000-0000-4000-8000-000000000001; USER=$(uuidgen | tr A-Z a-z)
TOKEN=$(curl -s -XPOST localhost:8080/v1/events/$EVENT/queue/join -d "{\"user_id\":\"$USER\"}" | jq -r .token)
curl -s -XPOST localhost:8080/v1/holds -H "X-Admission-Token: $TOKEN" \
     -d "{\"event_id\":\"$EVENT\",\"seat_id\":1,\"user_id\":\"$USER\"}" | jq .
# повтор на то же место другим пользователем → 409 seat_held
HOLD=$(curl -s -XPOST localhost:8080/v1/holds -H "X-Admission-Token: $TOKEN" -d "{\"event_id\":\"$EVENT\",\"seat_id\":2,\"user_id\":\"$USER\"}" | jq -r .id)
ORDER=$(curl -s -XPOST localhost:8080/v1/orders -H "Idempotency-Key: $(uuidgen)" -d "{\"hold_ids\":[\"$HOLD\"],\"user_id\":\"$USER\"}" | jq -r .id)
curl -s -XPOST localhost:8080/v1/orders/$ORDER/pay | jq .status          # "paid"
curl -s localhost:8080/v1/users/$USER/tickets | jq .
make smoke                            # 60 с, зелёный
make invariants                       # I1–I4 OK
```

**Фолбэк** (нет сети, модель тупит, вышли за 60-ю минуту):

```bash
git switch main                       # = тег lesson-1, эталонная реализация
git diff demo/act2-start..lesson-1 --stat
sed -n '1,80p' soldout/internal/booking/app/service.go
make up && make smoke && make invariants
```

## Акт 3 (64–70 мин): fitness function ловит отклонение

Из результата акта 2 (или с `main`):

```bash
cd soldout && claude
```

Промпт:

```
Быстро добавь в GET /v1/events/{id} поле sold_count — количество проданных билетов.
```

Ожидание (> 50 %): агент из `catalog` полезет в `booking/domain`, `booking/adapters/pg` или напрямую в таблицу `holds`/`tickets`. Затем:

```bash
make lint-arch                        # Component catalog_internal shouldn't depend on .../booking/domain
```

Промпт:

```
Проверка архитектуры упала. Исправь, не нарушая правило зависимостей из constitution.
```

Ожидание: агент использует `bookingapi.SoldCounter` (`booking.api.SoldCount(eventID)`), внедрённый в `catalog/app` через конструктор; `make lint-arch` зелёный, `curl localhost:8080/v1/events/$EVENT | jq .sold_count`.

**Фолбэк** — заготовленное нарушение:

```bash
git switch demo/act3-violation
make lint-arch                        # красный: catalog/app → booking/domain
git diff main..demo/act3-violation -- soldout/internal/catalog/app/service.go
# правим руками: import bookingdomain → bookingapi.SoldCounter; make lint-arch зелёный
git switch main
```

Вывод для слайда: ограничение, записанное в `.go-arch-lint.yml`, стоит 40 строк YAML и ловит нарушение за секунду; code review этого не гарантирует.

## После занятия

```bash
cd soldout && make storm              # 2.5 мин; ожидаемо КРАСНЫЙ по p99 и ошибкам — исходная точка занятия 2
make invariants                       # зелёный даже после шторма (двойных hold нет)
make down
```

## Замеры прогона (чистое состояние, `docker compose down -v`)

Прогон 05.09.2026 00:00 (MacBook Pro M4, Docker 12 CPU / 8 ГБ; образ уже собран — с холодной сборкой `make up` ≈ 45 с). Артефакты — `hw/hw1/artifacts/`.

| Шаг demo.md | Время, с | Результат |
|---|---|---|
| make down (чистое состояние) | 1.0 | OK |
| make up (сборка образа + старт + readyz) | 13.4 | OK |
| curl /readyz | 0.0 | OK |
| ручной сценарий: join → hold → 409 → order → pay → tickets | 0.2 | OK |
| make smoke (k6, 60 с) | 62.6 | OK |
| make invariants (после smoke) | 1.0 | OK |
| make lint-arch | 0.9 | OK |
| make test (unit + testcontainers) | 4.2 | OK |
| make storm (k6, 2 мин 20 с; ожидаемо красный) | 142.2 | красный по замыслу (k6 thresholds) |
| make invariants (после storm) | 13.2 | OK |
| make hot-row (k6, 60 с; ожидаемо красный) | 62.7 | красный по замыслу (k6 thresholds) |
| make invariants (после hot-row) | 17.2 | OK |
| акт 1: `git switch demo/act1-naive` + чтение кода | < 1 | — |
| акт 3 (фолбэк): `git switch demo/act3-violation && make lint-arch` | ~1 | красный по замыслу |

Замечания: `make invariants` сразу после storm/hot-row ждёт освобождения слотов PostgreSQL (пул 200 > `max_connections` 100 — ограничение 3 из ADR-001), поэтому 13–17 с вместо 1 с. Живая часть акта 2 (генерация агентом) в таблицу не входит: на прогоне 04.09 эталонная реализация писалась ~15 мин, лимит на занятии — 16 мин с фолбэком на тег.
