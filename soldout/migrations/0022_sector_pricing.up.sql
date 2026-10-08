-- 0022 (HW1, V2): ценовая политика сектора. Цена k-го места сектора = base * (1 + step_pct/100)^floor((k-1)/step_seats),
-- округление вниз до копейки. Нет строки — у сектора фиксированная цена (TICKET_PRICE_MINOR).
CREATE SCHEMA IF NOT EXISTS catalog;
CREATE TABLE catalog.sector_pricing (
    event_id         uuid   NOT NULL,
    sector_id        uuid   NOT NULL,
    base_price_minor bigint NOT NULL CHECK (base_price_minor > 0),
    step_seats       int    NOT NULL CHECK (step_seats > 0),
    step_pct         int    NOT NULL CHECK (step_pct >= 0),
    PRIMARY KEY (event_id, sector_id)
);
COMMENT ON TABLE catalog.sector_pricing IS 'catalog: динамическая цена сектора, растет на step_pct процентов каждые step_seats удержанных или проданных мест';
