-- 0010 (шаг A → C): одно место не может быть одновременно удержано и продано. uniq_active_hold защищал только
-- пару active/active, этот индекс закрывает окно «hold подтверждён, а на место уже вставили новый active».
-- Он же делает SeatSold и «свободно ли место» индексными проверками. Единственная гарантия I1 — partial unique index.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS holds_taken_uidx ON holds (event_id, seat_id) WHERE status IN ('active', 'confirmed');
