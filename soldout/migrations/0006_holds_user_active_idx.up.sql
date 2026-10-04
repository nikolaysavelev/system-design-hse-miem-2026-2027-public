-- 0006 (шаг A): лимит 4 билета проверяется на каждом hold — индекс под (event_id, user_id) по живым hold.
-- CONCURRENTLY — без блокировки записи, поэтому по одной команде на файл и вне транзакции.
CREATE INDEX CONCURRENTLY IF NOT EXISTS holds_user_active_idx ON holds (event_id, user_id) WHERE status IN ('active', 'confirmed');
