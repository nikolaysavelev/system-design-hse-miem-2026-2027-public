# Занятие 4 (26.09.2026): материалы

Материалы занятия лежат на `main`, от demo-ветки они не зависят. Код и стенд живут на ветках, по одной на акт, переключение идет только вперед:

| Ветка | Что на ней | Замеры |
|---|---|---|
| `demo/l4-problem` | почтовый шлюз `mailgw` в пути pay, сценарий `checkout`, `make pay-demo` | `act1/` |
| `demo/l4-otel` | OpenTelemetry, Tempo с MCP, дашборд `soldout-traces`, `make break-goroutine` | `act2/` |
| `demo/l4-cdc` | outbox, Debezium, Kafka, сервис `notifier`, `make kill-notifier`, `make poison`, `make repair-i5` | `act3/` |

| Файл | Зачем |
|---|---|
| `demo.md` | все команды по актам |
| `results.md` | цифры трех веток, сломы, критерии приемки |
| `ai-session.md`, `ai-session/` | промпты для агента и реальные ответы Tempo MCP и Grafana MCP |
| `../../hw/hw2/HW_2.md` | домашняя работа 2, срок: занятие 6 |

Документы soldout к занятию есть только на `demo/l4-cdc`, в каталоге `soldout/docs/`: `adr/ADR-003-event-delivery-and-notifier.md`, `asyncapi.yaml`, `observability.md`, обновленные `constitution.md` и `AGENTS.md`. Прочитать их, не переключая ветку:

```bash
git show demo/l4-cdc:soldout/docs/adr/ADR-003-event-delivery-and-notifier.md
```
