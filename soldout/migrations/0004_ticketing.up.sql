-- 0004_ticketing: таблица модуля ticketing. order_id/seat_id без FK (см. 0003).
CREATE TABLE tickets (
    id        uuid        PRIMARY KEY,
    order_id  uuid        NOT NULL,
    seat_id   bigint      NOT NULL,
    code      text        NOT NULL UNIQUE,
    issued_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE tickets IS 'ticketing: выпущенные билеты, (order_id, seat_id) уникальны — повторный Issue идемпотентен';
CREATE UNIQUE INDEX tickets_order_seat_uniq ON tickets (order_id, seat_id);
