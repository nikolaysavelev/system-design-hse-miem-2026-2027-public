# Занятие 4. ИИ-нить: агент ищет время через Tempo MCP и Grafana MCP

Два промпта для Claude Code в `soldout/` (`.mcp.json`: `postgres`, `tempo`, `grafana`). Ниже для каждого: промпт, какие инструменты агент должен вызвать, и реальные ответы MCP-серверов со стенда 26.09.2026 (сценарий занятия целиком, `results.md`). Ответы сохранены целиком в `ai-session/*.json`.

Как получены ответы: инструменты вызваны напрямую по протоколу MCP (Tempo: streamable HTTP `http://localhost:3200/api/mcp`, mcp-grafana 1.6.0: stdio в Docker, токен service account с ролью Viewer из `make grafana-token`) с теми аргументами, которые агент передает при таком промпте. Живой диалог агента на занятии будет своими словами; цифры в нем должны совпасть с этими.

## Подключение

```bash
cd soldout
make up                 # токен для mcp-grafana создается сам (make grafana-token)
claude                  # /mcp: postgres, tempo, grafana: connected
```

Если `tempo` не подключается по streamable HTTP: у mcp-grafana есть свои инструменты Tempo (`query_tempo_metrics`, `get_tempo_trace`, `list_tempo_attribute_names`). Если не работают оба: `make trace-json TRACE=<id>` и попросить агента прочитать файл `k6/results/trace-<id>.json`.

## Промпт 1 (акт 2, ветка `demo/l4-otel`, во время или после `make checkout-storm`)

> Через Tempo найди самые медленные запросы `POST /v1/orders/{id}/pay` за последние 15 минут. Возьми один трейс и объясни, на что уходит время: какие span'ы, сколько, что из этого внешние системы, а что база. Исправлений не предлагай, только диагноз.

### Вызов 1: `tempo.traceql-search`

```json
{"query": "{ name = \"POST /v1/orders/{id}/pay\" && duration > 2s }",
 "start": "2026-09-25T23:01:50Z", "end": "2026-09-25T23:03:50Z"}
```

Ответ (`ai-session/1-search.json`): 20 трейсов длительностью 2 024-2 550 мс. У каждого `serviceStats`: `{"mailgw": {"spanCount": 2, "errorCount": 1}, "soldout": {"spanCount": 32, "errorCount": 1}}`. Два span'а шлюза и одна ошибка у всех 20: каждый pay дольше 2 с содержит 502 от почтового шлюза и повтор.

### Вызов 2: `tempo.get-trace`

```json
{"trace_id": "127f6f2490763225d32dc2bf27045417"}
```

Ответ (`ai-session/2-get-trace.json`), span'ы длиннее 50 мс:

| Сервис | Span | Длительность | Атрибуты |
|---|---|---|---|
| soldout | `POST /v1/orders/{id}/pay` | 2 226 мс | `http.response.status_code=200` |
| soldout | `booking.PayOrder` | 2 226 мс | |
| soldout | `payment.Charge` → `psp.emulate` | 302 → 301 мс | `psp.delay_ms=300` |
| soldout | `ticketing.Issue` → `notification.Send` | 1 923 → 1 922 мс | |
| soldout | `mailgw.POST /send` | 926 мс | `retry.attempt=0`, `status=502`, Error |
| mailgw | `POST /send` | 926 мс | 502 |
| soldout | `mailgw.POST /send` | 996 мс | `retry.attempt=1`, `status=202` |
| mailgw | `POST /send` | 996 мс | 202 |

Ресурс: `service.version=b4519c3`, `deployment.environment=lab`.

### Вызов 3 (по желанию агента): `tempo.traceql-metrics-range`

```json
{"query": "{ resource.service.name = \"soldout\" && (name = \"mailgw.POST /send\" || name = \"psp.emulate\") } | quantile_over_time(duration, .5) by (name)",
 "start": "2026-09-25T23:01:00Z", "end": "2026-09-25T23:10:00Z"}
```

Ответ (`ai-session/4-metrics-stages.json`), максимум по минутам: `mailgw.POST /send` p50 0.84-1.07 с в 23:01-23:03; `psp.emulate` p50 0.38-0.54 с на всем интервале. После 23:03 серии `mailgw.POST /send` у soldout нет: на ветке cdc монолит больше не зовет шлюз.

### Какой ответ ждем от агента

pay 2.2 с. Из них 0.3 с PSP (`psp.emulate`, эмулятор in-process, но это внешняя остановка) и 1.9 с отправка письма внутри запроса: первая попытка к шлюзу 0.93 с закончилась 502, повтор 1.0 с. SQL-span'ы по миллисекунде, база ни при чем. Все 20 самых медленных pay устроены так же: хвост p99 делают повторы к шлюзу. Pay ждет две внешние системы последовательно, и одну из них (почту) пользователь в ответе не видит.

Что проговорить: квантили TraceQL-метрик приблизительные (экспоненциальные бакеты): p50 PSP 0.38 с при фактических 0.30 с. Для точных чисел нужен трейс или k6.

## Промпт 2 (после акта 4, ветка `demo/l4-cdc`)

> Сравни p99 оплаты по Prometheus за последний час: до и после перехода на outbox. Потом найди в Tempo трейс, где оплату и письмо видно вместе, и объясни по нему, куда делось время.

### Вызов 1: `grafana.query_prometheus`

```json
{"datasourceUid": "prometheus", "queryType": "range", "stepSeconds": 60,
 "expr": "histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{route=\"/v1/orders/{id}/pay\"}[1m])))",
 "startTime": "2026-09-25T22:59:00Z", "endTime": "2026-09-25T23:10:00Z"}
```

Ответ (`ai-session/5-prom-pay-p99.json`):

| Время (UTC) | p99 pay | Ветка |
|---|---|---|
| 23:00-23:04 | 2.49 с | problem, otel |
| 23:06-23:08 | 0.97-0.98 с | cdc, checkout-storm |
| 23:09-23:10 | 0.84-0.95 с | cdc, kill-notifier |

Там же `sum(rate(http_requests_total{route="/v1/orders/{id}/pay"}[1m]))` (`ai-session/6-prom-pay-rps.json`): до 120 pay/с на problem и otel, 186-235 pay/с на cdc.

### Вызов 2: `tempo.traceql-search` со структурным оператором

```json
{"query": "{ name = \"booking.PayOrder\" } >> { resource.service.name = \"notifier\" && name = \"notifier.consume\" }",
 "start": "2026-09-25T23:05:40Z", "end": "2026-09-25T23:07:45Z"}
```

Ответ (`ai-session/7-search-cross.json`): 20 трейсов 1.2-1.8 с, в каждом три сервиса: `{"soldout": 29 span'ов, "notifier": 11, "mailgw": 1}`. Оператор `>>` ищет потомка через границу процесса: `notifier.consume` дочерний к `booking.PayOrder`, потому что `traceparent` доехал заголовком Kafka.

### Вызов 3: `tempo.traceql-metrics-range`

```json
{"query": "{ resource.service.name = \"soldout\" && name = \"POST /v1/orders/{id}/pay\" } | quantile_over_time(duration, .99)",
 "start": "2026-09-25T23:01:00Z", "end": "2026-09-25T23:10:00Z"}
```

Ответ (`ai-session/3-metrics-pay.json`): 3.6-3.7 с в 23:01-23:03, 1.04-1.07 с в 23:05-23:09.

### Какой ответ ждем от агента

p99 pay упал примерно в 2.5 раза (Prometheus: 2.49 → 0.97 с; k6: 2.30 → 0.70 с), пропускная способность оплаты выросла с ~120 до ~190 в секунду. В трейсе после перехода pay заканчивается после PSP и commit; письмо обрабатывает другой сервис (notifier) через десятки миллисекунд после commit, и шлюз со своими 0.9-1.1 с и повторами идет там, вне запроса. Время не исчезло, оно ушло из пути пользователя: письмо приходит через 1.1 с (p50) после оплаты.

Что проговорить:

- Три инструмента дали три разных p99 (k6 0.70 с, Prometheus 0.97 с, TraceQL 1.05 с). Точный из них k6: гистограммы Prometheus и Tempo считают квантиль по бакетам (у `http_request_duration_seconds` соседние границы 0.5, 1, 2.5 с). Агенту надо сказать, откуда цифра и какая у нее точность.
- Провокация на остаток времени: "сделай pay еще быстрее". Ждем предложения убрать PSP из пути запроса тем же outbox'ом. Разобрать, почему нельзя: подтверждение оплаты нужно в ответе pay, иначе меняется продукт и контракт `api/openapi.yaml` (constitution, раздел 4; занятие 7, саги).
