-- 0022 (HW1, V3): групповой hold "друзья рядом". hold_groups — запрос на N смежных мест (ключ идемпотентности),
-- holds.group_id связывает hold'ы группы. NULL — обычный hold.
CREATE TABLE hold_groups (
    id              uuid        PRIMARY KEY,
    event_id        uuid        NOT NULL,
    sector_id       uuid        NOT NULL,
    user_id         uuid        NOT NULL,
    n               int         NOT NULL CHECK (n BETWEEN 2 AND 4),
    idempotency_key text        NOT NULL UNIQUE,
    created_at      timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE hold_groups IS 'booking: групповой hold на N смежных мест одного ряда, idempotency_key защищает POST /v1/holds/group от повторов';
ALTER TABLE holds ADD COLUMN group_id uuid;
