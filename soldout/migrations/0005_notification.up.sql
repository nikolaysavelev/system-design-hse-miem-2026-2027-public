-- 0005_notification: таблица модуля notification (на L1 — запись в таблицу + лог).
CREATE TABLE notifications (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL,
    kind       text        NOT NULL,
    payload    jsonb       NOT NULL,
    status     text        NOT NULL CHECK (status IN ('sent', 'failed')),
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE notifications IS 'notification: журнал отправленных уведомлений (эмулятор e-mail/push)';
CREATE INDEX notifications_user_id_idx ON notifications (user_id); -- выборка уведомлений пользователя
