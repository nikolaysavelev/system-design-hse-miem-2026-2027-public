-- 0002_booking: таблицы модуля booking — holds, orders, order_items.
-- Намеренно (ADR-001, «известные ограничения MVP»): нет индекса holds(seat_id) WHERE status='active'
-- и нет индекса holds(event_id, status) — карта зала и expirer читают holds последовательно.

CREATE TABLE holds (
    id         uuid        PRIMARY KEY,
    event_id   uuid        NOT NULL REFERENCES events (id),
    seat_id    bigint      NOT NULL REFERENCES seats (id),
    user_id    uuid        NOT NULL,
    status     text        NOT NULL CHECK (status IN ('active', 'released', 'confirmed')),
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE holds IS 'booking: удержание места на время оплаты (I1: не более одного active hold на (event, seat))';
-- Инвариант I1 на уровне БД: одно активное удержание на место.
CREATE UNIQUE INDEX uniq_active_hold ON holds (event_id, seat_id) WHERE status = 'active';

CREATE TABLE orders (
    id              uuid           PRIMARY KEY,
    event_id        uuid           NOT NULL REFERENCES events (id),
    user_id         uuid           NOT NULL,
    status          text           NOT NULL CHECK (status IN ('pending', 'paid', 'failed', 'expired')),
    amount          numeric(12, 2) NOT NULL,
    idempotency_key text           NOT NULL UNIQUE,
    created_at      timestamptz    NOT NULL DEFAULT now(),
    updated_at      timestamptz    NOT NULL DEFAULT now()
);
COMMENT ON TABLE orders IS 'booking: заказ; idempotency_key защищает POST /v1/orders от повторов';

CREATE TABLE order_items (
    order_id uuid NOT NULL REFERENCES orders (id),
    hold_id  uuid NOT NULL REFERENCES holds (id),
    PRIMARY KEY (order_id, hold_id)
);
COMMENT ON TABLE order_items IS 'booking: позиции заказа — ссылки на hold';
CREATE INDEX order_items_hold_id_idx ON order_items (hold_id); -- проверка «hold уже в заказе»
