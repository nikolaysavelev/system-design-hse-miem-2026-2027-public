-- 0021 (занятие 4): идемпотентный потребитель notifier. Уведомление — одно на (order_id, kind),
-- обработанные события — в notifier.processed_events (схема сервиса notifier, на стенде та же база, в проде — своя).
ALTER TABLE notifications ADD COLUMN order_id uuid;
ALTER TABLE notifications ADD COLUMN event_id uuid;
ALTER TABLE notifications ADD COLUMN attempts int NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN updated_at timestamptz;
ALTER TABLE notifications DROP CONSTRAINT notifications_status_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_status_check CHECK (status IN ('pending', 'sent', 'failed'));
CREATE UNIQUE INDEX notifications_order_kind_uniq ON notifications (order_id, kind); -- второе письмо на заказ невозможно

CREATE SCHEMA notifier;
CREATE TABLE notifier.processed_events (
    event_id     uuid        NOT NULL,
    consumer     text        NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, consumer)
);
COMMENT ON TABLE notifier.processed_events IS 'notifier: id событий, уже принятых потребителем (дедупликация at-least-once доставки)';
