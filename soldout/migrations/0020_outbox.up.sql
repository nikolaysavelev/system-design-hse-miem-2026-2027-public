-- 0020 (занятие 4): transactional outbox в формате Debezium EventRouter (id, aggregatetype, aggregateid, type, payload).
-- Владелец — platform/outbox. Событие пишется в той же транзакции, что и изменение агрегата (orders.paid).
-- Режим CDC: строка вставляется и удаляется в той же транзакции, в WAL остаётся INSERT, таблица всегда пуста.
-- Режим polling (lite): строка живёт, пока relay (notifier, NOTIFIER_SOURCE=outbox-poll) её не обработает.
CREATE TABLE outbox (
    id            uuid        PRIMARY KEY,
    aggregatetype text        NOT NULL,
    aggregateid   text        NOT NULL,
    type          text        NOT NULL,
    payload       jsonb       NOT NULL,
    traceparent   text,
    created_at    timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE outbox IS 'platform/outbox: события к доставке (Debezium CDC или polling relay), W3C traceparent едет в заголовок Kafka';
CREATE INDEX outbox_created_idx ON outbox (created_at); -- polling relay читает в порядке записи
ALTER TABLE outbox REPLICA IDENTITY DEFAULT;
CREATE PUBLICATION soldout_outbox FOR TABLE outbox; -- pgoutput: Debezium читает из WAL только эту таблицу
