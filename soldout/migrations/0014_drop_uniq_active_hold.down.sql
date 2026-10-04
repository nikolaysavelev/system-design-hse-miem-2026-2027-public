CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uniq_active_hold ON holds (event_id, seat_id) WHERE status = 'active';
