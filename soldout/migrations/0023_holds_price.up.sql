-- 0023 (HW1, V2): цена фиксируется в момент hold. ordinal — порядковый номер hold в секторе, price_minor — цена этого номера.
-- Для мероприятий без ценовой политики обе колонки NULL.
ALTER TABLE holds ADD COLUMN price_minor bigint;
ALTER TABLE holds ADD COLUMN ordinal int;
