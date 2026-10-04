-- 0001_init: расширения, таблицы модуля catalog (venues, sectors, seats, events) и queue (admissions).
-- Владелец каждой таблицы указан в COMMENT; чужие таблицы модули читают только через api.

CREATE EXTENSION IF NOT EXISTS pg_stat_statements; -- статистика запросов (занятие 2); требует shared_preload_libraries

CREATE TABLE venues (
    id   uuid PRIMARY KEY,
    name text NOT NULL
);
COMMENT ON TABLE venues IS 'catalog: площадки';

CREATE TABLE sectors (
    id       uuid PRIMARY KEY,
    venue_id uuid NOT NULL REFERENCES venues (id),
    name     text NOT NULL,
    kind     text NOT NULL CHECK (kind IN ('seated', 'standing')),
    capacity int  NOT NULL CHECK (capacity > 0)
);
COMMENT ON TABLE sectors IS 'catalog: секторы площадки; standing — танцпол (квота мест без нумерации ряда)';
CREATE INDEX sectors_venue_id_idx ON sectors (venue_id); -- запрос секторов площадки для карты зала

CREATE TABLE seats (
    id        bigint PRIMARY KEY,
    sector_id uuid   NOT NULL REFERENCES sectors (id),
    row_no    int    NOT NULL,
    seat_no   int    NOT NULL,
    UNIQUE (sector_id, row_no, seat_no)
);
COMMENT ON TABLE seats IS 'catalog: места; для standing-секторов row_no = 0, seat_no = 1..capacity';
-- индекс по sector_id покрывается UNIQUE (sector_id, row_no, seat_no)

CREATE TABLE events (
    id            uuid        PRIMARY KEY,
    name          text        NOT NULL,
    venue_id      uuid        NOT NULL REFERENCES venues (id),
    starts_at     timestamptz NOT NULL,
    sales_open_at timestamptz NOT NULL,
    sales_state   text        NOT NULL CHECK (sales_state IN ('scheduled', 'open', 'closed'))
);
COMMENT ON TABLE events IS 'catalog: мероприятия; venue_id связывает мероприятие со схемой зала';

CREATE TABLE admissions (
    token      text        PRIMARY KEY,
    event_id   uuid        NOT NULL REFERENCES events (id),
    user_id    uuid        NOT NULL,
    issued_at  timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
COMMENT ON TABLE admissions IS 'queue: допуски из waiting room; кэш токенов — в Valkey (admission:{token})';
