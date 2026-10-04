-- 0009 (шаг A): инвариант I2 на уровне БД и быстрый SeatSold по (event_id, seat_id).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS tickets_event_seat_uidx ON tickets (event_id, seat_id);
