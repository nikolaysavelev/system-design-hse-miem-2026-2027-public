-- 0012 (шаг B): одноразовый backfill проекции из holds при переходе на события. Единственное место, где
-- миграция catalog читает таблицу booking, — осознанно и один раз (после этого источник — только события).
INSERT INTO catalog_seat_state (event_id, seat_id, sector_id, state, version)
SELECT h.event_id, h.seat_id, s.sector_id,
       CASE h.status WHEN 'confirmed' THEN 'sold' ELSE 'held' END,
       1
FROM holds h
JOIN seats s ON s.id = h.seat_id
WHERE h.status IN ('active', 'confirmed')
ON CONFLICT (event_id, seat_id) DO NOTHING;

INSERT INTO catalog_sector_version (event_id, sector_id, version)
SELECT e.id, sec.id, 1
FROM events e
JOIN sectors sec ON sec.venue_id = e.venue_id
ON CONFLICT (event_id, sector_id) DO NOTHING;
