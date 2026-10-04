-- 0011 (шаг B): проекция карты зала — собственные таблицы catalog. Наполняется подписчиком из событий
-- booking.SeatStateChanged (platform/eventbus), catalog больше не читает holds/tickets.
-- Свободное место = отсутствие строки. Версия сектора инкрементируется в той же транзакции, что и проекция,
-- и служит ETag и версией записи в кэш (CAS).
CREATE TABLE catalog_seat_state (
    event_id   uuid        NOT NULL,
    seat_id    bigint      NOT NULL,
    sector_id  uuid        NOT NULL,
    state      text        NOT NULL CHECK (state IN ('held', 'sold')),
    version    bigint      NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, seat_id)
);
COMMENT ON TABLE catalog_seat_state IS 'catalog: проекция статусов мест (held/sold), источник — события booking';
CREATE INDEX catalog_seat_state_sector_idx ON catalog_seat_state (event_id, sector_id); -- карта сектора и сводка

CREATE TABLE catalog_sector_version (
    event_id   uuid        NOT NULL,
    sector_id  uuid        NOT NULL,
    version    bigint      NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (event_id, sector_id)
);
COMMENT ON TABLE catalog_sector_version IS 'catalog: версия карты сектора (ETag, CAS кэша), растёт на каждое изменение места сектора';
