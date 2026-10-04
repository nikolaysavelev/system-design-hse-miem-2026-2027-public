-- 0007 (шаг A): expirer читает просроченные active hold по expires_at — без индекса это seq scan каждые 5 с.
CREATE INDEX CONCURRENTLY IF NOT EXISTS holds_expires_active_idx ON holds (expires_at) WHERE status = 'active';
