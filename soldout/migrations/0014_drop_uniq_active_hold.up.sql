-- 0014 (шаг C): uniq_active_hold (event_id, seat_id) WHERE status='active' полностью покрыт holds_taken_uidx
-- (тот же ключ, предикат шире). Два уникальных индекса на одну пару — лишняя запись на write path и источник
-- ошибки в гонке: ON CONFLICT DO NOTHING обрабатывает только индексы-арбитры, а второй индекс поднимает исключение.
-- Единственная гарантия I1 теперь — holds_taken_uidx.
DROP INDEX CONCURRENTLY IF EXISTS uniq_active_hold;
