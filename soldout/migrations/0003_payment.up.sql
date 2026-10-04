-- 0003_payment: таблица модуля payment.
-- order_id без FK на orders: платежи и билеты уедут в отдельные сервисы/БД (занятие 3), FK между модулями не заводим.
CREATE TABLE payments (
    id              uuid        PRIMARY KEY,
    order_id        uuid        NOT NULL,
    status          text        NOT NULL CHECK (status IN ('succeeded', 'failed')),
    psp_ref         text,
    idempotency_key text        NOT NULL UNIQUE,
    created_at      timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE payments IS 'payment: результат обращения к платёжному эмулятору, idempotency_key — ключ повтора';
CREATE INDEX payments_order_id_idx ON payments (order_id); -- чтение платежа по заказу
