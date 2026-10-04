-- 0019 (занятие 4): полный checkout запрашивает билеты пользователя после каждой оплаты (GET /v1/users/{id}/tickets →
-- booking.list_user_orders). Без индекса это Seq Scan по orders: цена растёт с каждым заказом (5 мс на 30 000 заказов),
-- и после нескольких прогонов база упирается в CPU. CONCURRENTLY — по одной команде на файл, вне транзакции.
CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_user_created_idx ON orders (user_id, created_at DESC);
