-- 0008 (шаг A): tickets получает event_id, чтобы инвариант I2 («один билет на (event, seat)») держался индексом,
-- а не JOIN'ом с orders. Backfill из orders — один раз при миграции (не CONCURRENTLY: обычная транзакция).
ALTER TABLE tickets ADD COLUMN IF NOT EXISTS event_id uuid;
UPDATE tickets t SET event_id = o.event_id FROM orders o WHERE o.id = t.order_id AND t.event_id IS NULL;
ALTER TABLE tickets ALTER COLUMN event_id SET NOT NULL;
COMMENT ON COLUMN tickets.event_id IS 'ticketing: денормализовано из заказа для уникального индекса (event_id, seat_id)';
