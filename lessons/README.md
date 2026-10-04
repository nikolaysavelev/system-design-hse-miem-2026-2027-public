# Занятия: соответствие папок, веток и записей

Папки в `lessons/` названы по архитектурным шагам системы `soldout`. Один шаг может занимать два занятия, поэтому номера папок и номера занятий различаются. Эта страница связывает одно с другим и дает оглавление записей.

## Карта

| Занятие | Папка | Ветки состояния системы | Что в папке |
|---|---|---|---|
| 1 | `lessons/01` | `demo/act1-naive`, `demo/act2-start`, `demo/act3-violation`, тег `lesson-1` | `demo.md`, `load-model.md` (расчет нагрузки по шагам), `sequences.md` и `diagrams/` (последовательности hold, pay, очереди), `act1-review.md` (дефекты наивной реализации), `adr/ADR-001` |
| 2 | `lessons/toolkit` | те же | обзор инструментария агентов, ссылки и порядок установки |
| 3 | `lessons/02` | `demo/l2-baseline` → `stepA` → `stepB` (показаны), `stepC`, `stepD` (в материалах) | `results.md`, `demo.md`, `act1/`, `act2/`, `act3/` с замерами (k6, pg_stat_statements, профили, скриншоты), `baseline.md` |
| 4 | `lessons/04`, акты 1–2 | `demo/l4-problem` → `demo/l4-otel` | `demo.md`, `results.md`, `act1/` (проблема), `act2/` (трейсинг, `trace-waterfall.png`, `break-goroutine.txt`), `ai-session.md` |
| 5 | `lessons/04`, акт 3; `lessons/05/diagrams` | `demo/l4-cdc` | `act3/` (outbox, Debezium, Kafka, notifier: `pay-demo-cdc.txt`, `trace-cdc.png`, `kill-notifier.txt`, `poison.txt`), диаграммы 01–04 |
| 6 (план) | `lessons/04` акт 3, `lessons/05` акт 1 | `demo/l5-k8s` | сломы cdc и ADR-003; реплики в compose, `act1/` |
| 7 (план) | `lessons/05` | `demo/l5-k8s`, `deploy/` | кластер, выкатка, автоскейлинг, агент и кластер, ADR-004 |

Решения с ценой: `soldout/docs/adr/`. Правила проекта: `soldout/docs/constitution.md`, `soldout/AGENTS.md`. Как поднять стенд: `docs/setup.md`; кластер: `docs/deploy.md`.

## Записи: оглавление

### Занятие 1. Теория: требования, модули, HLD, ADR

- 00:09:57 О курсе: идея
- 00:13:53 О курсе: регламент
- 00:18:52 Обсуждение с группой
- 00:30:00 Системный дизайн все еще на человеке
- 00:33:22 Кейс: продажа билетов на концерт
- 00:41:12 Функциональные требования
- 00:46:59 Разбор кейса
- 00:51:13 Нефункциональные требования и расчет нагрузки (`lessons/01/load-model.md`)
- 00:53:35 Модули: таблица ответственности и владения данными
- 01:02:23 HLD: C4, контейнеры
- 01:04:10 ADR (`lessons/01/adr/ADR-001-modular-monolith.md`)
- 01:09:29 Акт 1: агент без спецификации (`lessons/01/act1-naive`, `act1-review.md`)
- 01:13:36 Акт 2: агент со спецификацией
- 01:19:21 Итог: системный дизайн все еще на человеке

### Занятие 2. Практика: инструментарий агентов

- 00:07:22 Hugging Face: каталог моделей, inference providers
- 00:13:12 skills.sh: экосистема Agent Skills, лидерборд
- 00:16:56 Каталог MCP-серверов, подключение к Cursor, VS Code, Copilot CLI
- 00:20:20 ADR-001, модульный монолит soldout, разбор ошибки миграции
- 00:25:12 pi.dev: минималистичный агентный харнесс
- 00:28:52 OpenRouter: модели, выпуск API-ключа
- 00:37:50 Расширения VS Code и список MCP-серверов
- 00:41:18 Структура занятия 1, act1-naive
- 00:53:52 Модули: таблица ответственности
- 01:05:30 HLD: C4, контейнеры
- 01:11:30 Architecture decision record
- 01:15:46 Сравнение результатов агента, CLAUDE.md, сборка проекта
- 01:19:46 Контейнеры стенда soldout
- 01:23:08 Grafana Pyroscope: профилирование памяти

Материалы: `lessons/toolkit/README.md`, `docs/ai-agents.md`.

### Занятие 3. Практика: нагрузка, метрики, оптимизация

- 00:04:34 HLD, постановка задачи
- 00:18:22 k6: сценарий storm.js, пороги и стадии (`soldout/k6/storm.js`)
- 00:21:52 Claude Code в VS Code, запуск агента
- 00:25:14 Grafana, дашборд soldout RED: RPS, ошибки 5xx, p99, пул pgx
- 00:29:04 architecture.md: вход в waiting room, проверка допуска, hold места
- 00:32:52 Инварианты и диаграммы последовательности
- 00:40:08 Код каталога: SeatsByVenue, запрос мест зала
- 00:47:12 Grafana: heap и GOMEMLIMIT, горутины, GC, acquire timeouts
- 00:53:36 Логи soldout-pgbouncer
- 00:57:32 Grafana: p99, SQL p99 по запросу, кэш карты зала
- 01:01:02 Сравнение прогонов, дефекты исправления, повторное тестирование
- 01:08:18 service.go: refreshAsync, BuildSectorSeatMap
- 01:12:16 Grafana: контенция hold, шина событий, память и горутины
- 01:21:18 Результаты трех прогонов: baseline, шаг A, шаг B (`lessons/02/act3/README.md`)

### Занятие 4. Продолжаем улучшать систему с ИИ-агентом

Первая треть записи отсутствует: на ней показывался сценарий `make checkout-storm` на ветке `demo/l4-problem` и то, что инструменты занятия 3 не находят причину (`lessons/04/act1/README.md`).

- 00:13:24 Grafana soldout RED: RPS и ошибки по маршрутам
- 00:16:16 Латентность и пул pgx
- 00:21:28 architecture.md, диаграмма последовательности, анализ hold 201/409 с агентом
- 00:24:12 Контейнер soldout-mailgw-1, логи
- 00:26:52 Стенд: лимиты CPU и памяти
- 00:28:22 OpenTelemetry: что это
- 00:31:58 make checkout-storm
- 00:33:30 ИИ-нить: агент ищет время через Tempo MCP и Grafana MCP
- 00:35:08 Tempo MCP server: список инструментов
- 00:36:48 Разбор трейса POST /v1/orders/{id}/pay (`lessons/04/act2/trace-waterfall.png`)
- 00:40:14 Tempo: span'ы booking.PayOrder
- 00:42:56 Рекомендации агента по переработке
- 00:44:42 План: убрать уведомление с пути запроса через outbox
- 00:49:12 Tempo: span'ы payment.Charge

### Занятие 5. Асинхронщина, CDC, Outbox

- 00:07:32 Разбор trace-waterfall.png из занятия 4
- 00:12:44 Схема базы soldout
- 00:17:00 Диаграмма 01: сценарий оплаты до изменений (`lessons/05/diagrams/01-pay-before.png`)
- 00:22:22 Диаграмма 02: уведомление в горутине, dual write
- 00:24:28 break-goroutine: авария после commit, нарушен инвариант I5 (`lessons/04/act2/break-goroutine.txt`)
- 00:27:56 Диаграмма 03: оплата через outbox и CDC
- 00:36:00 Диаграмма 04: архитектура после CDC
- 00:39:44 Контейнеры стенда: notifier, connect, kafka-ui
- 00:43:12 Таблица outbox и ее столбцы
- 00:47:44 make cdc-status: слот репликации, коннектор, логи notifier
- 00:50:36 make trace-last, разбор ошибки скрипта с агентом
- 00:55:00 Эскиз transactional outbox: App, Worker, PostgreSQL, Kafka, Destination
- 01:02:50 Трейс после make pay-demo в Grafana Explore
- 01:06:24 Документация Debezium: архитектура
- 01:11:04 Диаграмма 03 и вывод trace-last
- 01:23:36 spec/soldout.md: НФТ; итоги make checkout-storm (`lessons/04/results.md`)
- 01:25:46 Логи notifier
