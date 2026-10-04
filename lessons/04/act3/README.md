# Акт 4 (ветка cdc). Outbox, Debezium, Kafka, notifier

Ветка `demo/l4-cdc`, прогон 26.09.2026 02:05 по сценарию занятия на одной БД: `make up` на `demo/l4-problem` → `checkout-storm` → `make switch BRANCH=demo/l4-otel` → `checkout-storm` → `break-goroutine` → `make switch BRANCH=demo/l4-cdc` → `repair-i5` → `checkout-storm` → `kill-notifier` → `poison`. Сводка трех веток: `../results.md`.

## Трейс через Kafka

`trace-cdc.png`, `trace-cdc-waterfall.txt`, `trace-cdc.json`: один trace_id на три сервиса.

| Этап | Время |
|---|---|
| `POST /v1/orders/{id}/pay` (soldout) | 326 мс, из них `psp.emulate` 302 мс |
| `outbox.insert` + `outbox.delete` в транзакции pay | 1 + 1 мс, событие `outbox.append` на `booking.PayOrder` |
| от конца pay до `notifier.consume` (WAL → Debezium → Kafka → опрос) | 59 мс |
| `notifier.consume` → `notification.Send` → `mailgw.POST /send` | 1 139 мс, из них шлюз 1 130 мс |

Почта ушла из пути запроса целиком: pay ждет только PSP.

## checkout-storm

| | problem | cdc |
|---|---|---|
| pay p50 / p95 / p99 / max | 1 217 / 1 630 / 2 301 / 2 608 мс | **487 / 605 / 703 / 807 мс** |
| Покупок за 90 с | 10 818 (118/с) | **16 891 (187/с)** |
| Ошибки | 0 % | 0 % |
| order p99, tickets p99 | 105 мс, 2 мс | 182 мс, 177 мс |
| CPU PostgreSQL (лимит 1 CPU) | 45-59 % | **102-109 %** |

Порог `pay p(99)<800` зеленый. Узкое место переехало: pay больше не ждет почту, цикл покупки крутится в 1.6 раза быстрее, и база с лимитом 1 CPU насыщается (order и tickets p99 выросли до 180 мс). Walsender Debezium тут ни при чем: в отдельном эксперименте с коннектором на паузе база загружалась так же.

## Доставка писем

| Метрика | Значение | Файл |
|---|---|---|
| Лаг notifier под checkout-storm | 212-455 сообщений (в среднем 337 при ~187 событиях/с, около 1.8 с) | `notifier-lag.txt` |
| Задержка письма от commit оплаты до `sent`, p50 / p99 | 1.14 / 2.65 с (шлюз сам по себе 0.9 с ± 30 %, плюс повтор при 502) | `notifier-lag.txt`, дашборд `soldout-events` |
| `SELECT count(*) FROM outbox` | 0 в начале и в конце | `cdc-status-*.txt` |
| Слот `soldout_outbox_slot` | active, `wal_behind` 72 МБ после шторма | `cdc-status-end.txt` |

Лаг считается в сообщениях после зафиксированного смещения, а смещение фиксируется после отправки письма: письма, которые еще идут через шлюз, входят в лаг. Спека ждала лаг меньше 2 с: в среднем 1.8 с, на пиках 2.4 с.

`wal_behind` 72 МБ при пустой outbox: подтвержденная позиция слота двигается только на событиях outbox и heartbeat (раз в 10 с), а WAL шторма пишется непрерывно. Если остановить Connect, эта цифра растет без ограничений.

## Сломы

**`make kill-notifier`** (`kill-notifier.txt`, `kill-notifier-lag.txt`, `events-dashboard.png`): checkout 90 с, notifier остановлен на 20-й секунде и запущен на 60-й. Pay не заметил (p99 588 мс, 0 % ошибок), лаг вырос примерно до 10 800 сообщений и ушел в 0 примерно за минуту. Итог: paid 58 794 = notifications 58 794, дублей 0, неотправленных 0. Пока notifier лежит, лаг показывает отдельный экспортер `notifier-lag`, без него график оборвался бы вместе с сервисом.

**`make poison`** (`poison.txt`): сообщение с невалидным payload, 5 попыток с паузами 0.5, 1, 2, 4 с, затем DLQ с заголовками `error`, `attempts`, `original_topic`, `original_partition`, `original_offset`. Остальные сообщения, в том числе той же партиции, обрабатываются параллельно; смещение партиции 0 не фиксируется дальше отравленного сообщения, пока оно не уйдет в DLQ.

**`make repair-i5`** (`repair-i5.txt`): после `break-goroutine` на ветке otel один оплаченный заказ остался без письма. Сверка нашла его, записала `OrderPaid` в outbox, Debezium и notifier доставили письмо, I5 зеленый. В прошлых прогонах таких заказов было 27 и 43: письма, тихо потерянные еще в синхронном режиме (таймаут 3 с отменял запись уведомления), сверка дослала их тем же путем.

**Инварианты после всего** (`invariants.txt`): I1-I6 зеленые.

## Память стенда

Сумма по контейнерам на пике 3.0 ГБ (`docker-mem.txt`): Kafka 423 МБ, Connect 456 МБ, kafka-ui 280 МБ, notifier 25 МБ, Tempo 978 МБ. Tempo держал 978 МБ при лимите 1 ГБ, поэтому лимит поднят до 1.5 ГБ с `GOMEMLIMIT`; в более раннем прогоне с 512 МБ его убил OOM.

## Прочее

`k6-checkout-storm.txt`, `k6-storm-bg.txt`, `checkout-storm.json`, `pg-top.txt`, `pprof-top.txt`, `docker-stats.txt`: те же инструменты, что в актах 1 и 2. `pay-demo-cdc.txt`: pay 317 мс при одиночном запросе.
