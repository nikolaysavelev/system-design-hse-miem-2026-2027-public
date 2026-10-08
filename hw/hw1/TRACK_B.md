# HW1, трек Б: вариация V3 "друзья рядом"

Ветка `hw1/v3-base` построена от тега `lesson-5` и содержит готовую реализацию вариации V3 из `variants.md`. Ваша часть работы: спецификация, ADR-101, ревью реализации против спецификации, измерение, диагноз и две оптимизации (этапы 1, 3 и 4 из `HW_1.md`). Сценарий нагрузки на новый путь вы пишете сами.

## Что реализовано

Легенда: запрос "N мест рядом в секторе S" (N от 2 до 4). Удерживается непрерывный отрезок мест одного ряда атомарно, при неудаче не удерживается ничего. Все хотят первый ряд.

- `POST /v1/holds/group` ищет первый свободный отрезок из N мест одного ряда, начиная с `prefer_row` (по умолчанию с первого ряда), и удерживает его в одной транзакции. Занятыми считаются места с active или confirmed hold'ом и места с выпущенным билетом.
- Если места отрезка заняли конкуренты между поиском и вставкой, транзакция откатывается и поиск повторяется, всего до 5 попыток. Потом ответ 409 `no_adjacent_seats`.
- Hold'ы группы связаны `holds.group_id`, сама группа и ее `Idempotency-Key` лежат в `hold_groups`. Повтор с тем же ключом возвращает ту же группу.
- Нужен admission-токен, как для обычного hold. Лимит 4 билета на пользователя учитывает всю группу.
- Группа снимается, заказывается и истекает целиком: `DELETE /v1/holds/group/{group_id}`, `POST /v1/orders` с `group_id`, expirer дополняет батч остальными hold'ами тех же групп. Снять один hold группы или заказать часть группы нельзя (422 `group_partial`).
- Seed: продажи мероприятия 3 открыты, на нем удобно стрелять групповыми hold'ами. Мероприятия 1 и 2 работают как раньше, все цели занятий 3-5 (`storm`, `checkout`, `pay-demo`) не менялись.

Отличие от `lesson-5` по конфигурации: `EVENTBUS_BUFFER=40000` в `soldout/compose/lesson-04.yml` (как на `demo/l5-k8s`), иначе на `make storm` переполняется буфер шины событий карты зала и I6 краснеет независимо от вариации.

## Эндпоинты

| Метод и путь | Что делает |
|---|---|
| `POST /v1/holds/group` | тело `{event_id, sector_id, user_id, n, prefer_row?}`, заголовки `X-Admission-Token`, `Idempotency-Key`; 201 с группой (`group_id`, `row`, `holds`), 200 при повторе, 409 `no_adjacent_seats` |
| `DELETE /v1/holds/group/{group_id}` | снять все active hold'ы группы, 204 |
| `POST /v1/orders` | как раньше, либо `{group_id, user_id}` вместо `hold_ids` |

Контракт: `soldout/api/openapi.yaml` (схемы `GroupHold`, `Hold.group_id`).

## Где код

| Путь | Что там |
|---|---|
| `soldout/migrations/0022_hold_groups.*` | таблица `hold_groups`, колонка `holds.group_id` |
| `soldout/seed/seed.sql` | продажи мероприятия 3 открыты |
| `soldout/internal/booking/domain/adjacent.go` | поиск отрезка (`FindAdjacent`), размер группы и лимит; unit-тесты в `adjacent_test.go` |
| `soldout/internal/booking/app/service.go` | `CreateGroupHold`, `ReleaseGroup`, заказ по `group_id`, запрет частичных операций |
| `soldout/internal/booking/adapters/pg/store.go` | `sector_seats`, `sector_taken_seats`, `insert_group`, `insert_group_holds`, групповой expirer |
| `soldout/internal/booking/adapters/http/handlers.go` | маршруты `/v1/holds/group` |
| `soldout/internal/booking/adapters/pg/store_integration_test.go` | 40 конкурентов на первый ряд, идемпотентность, лимит, снятие, оплата и истечение группы целиком, "при неудаче ничего" |
| `soldout/tools/invariant-checker/main.go` | инвариант I8 |

## Инвариант I8

Проверяется `make invariants` по умолчанию: active и confirmed hold'ы каждой группы лежат в одном ряду сектора группы и образуют непрерывный отрезок номеров; число таких hold'ов равно N группы или нулю (группы, удержанной частично, нет).

## Как поднять и проверить

```bash
cd soldout
make up                  # полный стенд; make up-lite — 3 × 4 000 мест без obs-профиля
make invariants          # I1-I6, I8
make lint-all test

E=10000000-0000-4000-8000-000000000003; U=$(uuidgen | tr A-Z a-z)
T=$(curl -s -XPOST localhost:8080/v1/events/$E/queue/join -d "{\"user_id\":\"$U\"}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
curl -s -XPOST localhost:8080/v1/holds/group -H "X-Admission-Token: $T" -H "Idempotency-Key: $(uuidgen)" \
  -d "{\"event_id\":\"$E\",\"sector_id\":\"30000000-0000-4000-8000-000000000bb9\",\"user_id\":\"$U\",\"n\":3,\"prefer_row\":1}"
```

Идентификаторы в seed детерминированы: мероприятие 3 имеет id `10000000-0000-4000-8000-000000000003`, сектор s мероприятия 3 имеет id `30000000-0000-4000-8000-` плюс `hex(3000 + s)` с дополнением до 12 знаков. В seated-секторе 1 000 мест: 20 рядов по 50, два последних сектора площадки (танцпол) без рядов, групповой hold там не найдет отрезка. Общие помощники для сценария k6 лежат в `soldout/k6/lib.js` (`EVENT_ID`, `EVENT_NO=3`).
