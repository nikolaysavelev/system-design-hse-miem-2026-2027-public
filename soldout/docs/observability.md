# Наблюдаемость soldout: что где смотреть

Одна страница для занятия 4 и домашней работы. Порядок поиска всегда один: RED → span-метрики → трейс → лог.

## Где что

| Вопрос | Инструмент | Где |
|---|---|---|
| Все ли хорошо у пользователя? | RED: RPS, 5xx, p99 по маршрутам | Grafana `soldout-red` (http://localhost:3000/d/soldout-red) |
| Какой этап запроса медленный, в среднем по всем запросам? | span-метрики (connector `span_metrics` в otel-collector) | Grafana `soldout-traces` |
| Куда ушло время в одном конкретном запросе? | трейс (водопад) | Grafana Explore → Tempo; `make trace-last` |
| Что случилось в этом запросе, с деталями? | лог с `trace_id` | `make logs`, `docker compose logs soldout \| grep <trace_id>` |
| Успевает ли доставка событий? | лаг notifier, задержка письма, DLQ | Grafana `soldout-events`; `make cdc-status`; kafka-ui http://localhost:8088 |
| Где горит CPU? | профиль | `make pprof-top`, Pyroscope http://localhost:4040 |
| Что делает база? | `pg_stat_statements`, `pg_stat_activity` | `make pg-top`, `make pg-activity` |

Профиль показывает, где горит CPU. Трейс показывает, где ждем. Под нагрузкой ждем чаще, чем горим: время в очереди на соединение, в PSP, в почтовом шлюзе не видно ни в профиле, ни в `pg_stat_statements`.

## Как найти трейс

- **Последний pay:** `make trace-last` печатает trace_id и ссылку на Grafana Explore.
- **Медленные:** `make traces-slow` (TraceQL `{ name = "POST /v1/orders/{id}/pay" && duration > 1s }` за 5 минут). Свой запрос: `make traces-slow Q='{ name = "mailgw.POST /send" && span.retry.attempt = 1 }'` (pay с повтором к шлюзу).
- **По заказу:** `{ span.order.id = "<order_id>" }`, по пользователю: `{ span.enduser.id = "<user_id>" }`.
- **По логу:** в каждой строке лога запроса есть `trace_id=...`, вставьте его в поле TraceQL.
- **В файл:** `make trace-json TRACE=<id>` пишет `k6/results/trace-<id>.json`.
- **Из Claude Code:** MCP-сервер Tempo (`.mcp.json`, `http://localhost:3200/api/mcp`): инструменты `traceql-search`, `get-trace`, `traceql-metrics-range`. Запасной путь: `mcp-grafana` (инструменты `*_tempo_*`, `query_prometheus`), токен создает `make grafana-token`.

## Что есть в трейсе pay

`POST /v1/orders/{id}/pay` (сервер, имя по шаблону маршрута) → `booking.PayOrder` → SQL-span'ы с именем из комментария запроса (`booking.update_order` и т.п., то же имя, что в `db_query_duration_seconds` и `pg_stat_statements`) и `pgx.acquire` (ожидание соединения пула) → `payment.Charge` → `psp.emulate`. На ветке cdc: событие `outbox.append` на `booking.PayOrder`, затем в сервисе notifier `notifier.consume` → `notification.Send` → `mailgw.POST /send` (клиент) → `POST /send` (сервер mailgw). Повтор к шлюзу: отдельный span с атрибутом `retry.attempt`.

Контекст через Kafka едет заголовком `traceparent`: Debezium копирует колонку `outbox.traceparent` в заголовок сообщения, notifier извлекает его и открывает `notifier.consume` (SpanKind=consumer) дочерним к `booking.PayOrder`.

## Sampling

На стенде 100 % (`OTEL_TRACES_SAMPLER_ARG=1.0`): каждый трейс доступен для разбора. Цена на `checkout-storm`: медиана pay не изменилась, хвост вырос (p99 +8 %, `lessons/04/act2/README.md`), CPU приложения +8 п.п. В проде так не делают: head-sampling 1-10 % (`ParentBased(TraceIDRatioBased)`, решение принимает корневой сервис, остальные его наследуют) плюс tail-sampling в коллекторе, который оставляет все медленные и ошибочные трейсы. NFR-8 требует 100 % в окне продаж: это вопрос бюджета на хранение, его решают tail-sampling и короткий retention.

## Правила (constitution §6)

- Каждый входящий запрос и каждое потребленное сообщение имеют trace_id; логи несут trace_id.
- Новый внешний вызов оборачивается в span (`otel.StartClient` или `otel.Transport` для HTTP).
- Имена span'ов низкой кардинальности: шаблон маршрута, имя запроса, никаких id в имени, id пишутся в атрибуты.
- В атрибуты и логи не попадает PII: только `user_id`.
