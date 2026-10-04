-- 0013 (шаг C): ключи идемпотентности POST /v1/holds/any — в той же транзакции, что и hold.
-- Повтор с тем же ключом возвращает сохранённый hold, повтор с другим телом (request_hash) — 422.
CREATE TABLE idempotency_keys (
    user_id      uuid        NOT NULL,
    key          text        NOT NULL,
    request_hash text        NOT NULL,
    hold_id      uuid        REFERENCES holds (id),
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);
COMMENT ON TABLE idempotency_keys IS 'booking: идемпотентность /v1/holds/any (user_id, Idempotency-Key) → hold';
