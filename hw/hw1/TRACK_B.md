# HW1, трек Б: вариация V2 "динамическая цена"

Ветка `hw1/v2-base` построена от тега `lesson-5` и содержит готовую реализацию вариации V2 из `variants.md`. Ваша часть работы: спецификация, ADR-101, ревью реализации против спецификации, измерение, диагноз и две оптимизации (этапы 1, 3 и 4 из `HW_1.md`). Сценарий нагрузки на новый путь вы пишете сами.

## Что реализовано

Легенда: цена сектора растет на X % каждые N удержанных или проданных мест. Цена фиксируется в момент hold и не меняется до оплаты. Сумма заказа равна сумме цен его hold'ов. Два конкурентных hold'а на границе ступени не получают разную цену за один порядковый номер.

- Ценовая политика сектора лежит в таблице `catalog.sector_pricing(event_id, sector_id, base_price_minor, step_seats, step_pct)`. Если строки нет, сектор продается по фиксированной цене `TICKET_PRICE_MINOR`, как раньше.
- Цена k-го места сектора: `base * (1 + X/100)^floor((k-1)/N)`, округление вниз до копейки.
- Hold в секторе с политикой получает порядковый номер в секторе (`holds.ordinal`) и цену этого номера (`holds.price_minor`). Порядковый номер равен числу active и confirmed hold'ов сектора плюс один.
- Заказ: `amount_minor` равен сумме `price_minor` его hold'ов. Hold без цены (сектор без политики) стоит `TICKET_PRICE_MINOR`.
- Seed: продажи мероприятия 3 открыты, у всех его секторов политика N = 200, X = 5, база 5 000 ₽. Мероприятия 1 и 2 работают как раньше, все цели занятий 3-5 (`storm`, `checkout`, `pay-demo`) не менялись.

Отличие от `lesson-5` по конфигурации: `EVENTBUS_BUFFER=40000` в `soldout/compose/lesson-04.yml` (как на `demo/l5-k8s`), иначе на `make storm` переполняется буфер шины событий карты зала и I6 краснеет независимо от вариации.

## Эндпоинты

| Метод и путь | Что делает |
|---|---|
| `POST /v1/holds` | как раньше; в секторе с политикой в ответе появляются `ordinal` и `price_minor` |
| `GET /v1/events/{id}/sectors/{sector_id}/price` | цена следующего места сектора: `next_ordinal`, `price_minor`, `price` и параметры политики; 404 `no_pricing`, если у сектора нет политики |
| `POST /v1/orders` | как раньше; сумма считается по ценам hold'ов |

Контракт: `soldout/api/openapi.yaml` (схемы `Hold`, `SectorPrice`).

## Где код

| Путь | Что там |
|---|---|
| `soldout/migrations/0022_sector_pricing.*`, `0023_holds_price.*` | схема `catalog`, таблица политики, колонки `holds.price_minor` и `holds.ordinal` |
| `soldout/seed/seed.sql` | политика для секторов мероприятия 3, продажи мероприятия 3 открыты |
| `soldout/internal/booking/domain/pricing.go` | правило цены (`Pricing.PriceFor`), unit-тесты в `pricing_test.go` |
| `soldout/internal/booking/domain/order.go` | сумма заказа по ценам hold'ов |
| `soldout/internal/booking/app/service.go` | `CreateHold` (порядковый номер и цена), `SectorPrice` |
| `soldout/internal/booking/adapters/pg/store.go` | запросы `sector_pricing_for_update`, `sector_pricing`, `count_sector_taken` |
| `soldout/internal/booking/adapters/http/handlers.go` | маршрут `.../price` |
| `soldout/internal/booking/adapters/pg/store_integration_test.go` | 30 конкурентных hold'ов на границах ступеней: номера 1..30 без повторов, цены и сумма заказа верны |
| `soldout/tools/invariant-checker/main.go` | инвариант I7 |

## Инвариант I7

Проверяется `make invariants` по умолчанию:

1. у каждого заказа, все hold'ы которого имеют цену, `amount = Σ price_minor`;
2. у каждого hold'а в секторе с политикой `price_minor` равна цене его `ordinal`;
3. `ordinal` уникален среди active и confirmed hold'ов сектора.

## Как поднять и проверить

```bash
cd soldout
make up                  # полный стенд; make up-lite — 3 × 4 000 мест без obs-профиля
make invariants          # I1-I7
make lint-all test

# цена следующего места в секторе 1 мероприятия 3
curl -s localhost:8080/v1/events/10000000-0000-4000-8000-000000000003/sectors/30000000-0000-4000-8000-000000000bb9/price
```

Идентификаторы в seed детерминированы: мероприятие 3 имеет id `10000000-0000-4000-8000-000000000003`, сектор s мероприятия 3 имеет id `30000000-0000-4000-8000-` плюс `hex(3000 + s)` с дополнением до 12 знаков. Места мероприятия 3 имеют id `2N+1..3N` (N = 80 000 в `make up`, 4 000 в `make up-lite`), по 1 000 мест в секторе, в seated-секторе 20 рядов по 50 мест. Общие помощники для сценария k6 лежат в `soldout/k6/lib.js` (`EVENT_ID`, `EVENT_NO=3`).
