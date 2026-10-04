-- Сверка I5 (занятие 4): оплаченные заказы без уведомления получают событие OrderPaid в outbox.
-- Дальше обычный путь: Debezium → Kafka → notifier. Режим CDC: строки удаляются в той же транзакции.
-- Так чинится след make break-goroutine: пока письма шли не через outbox, повторить их было не из чего.
BEGIN;
CREATE TEMP TABLE repair ON COMMIT DROP AS
SELECT gen_random_uuid() AS id, o.id AS order_id, o.event_id, o.user_id, (o.amount * 100)::bigint AS amount_minor,
       coalesce(array_agg(h.seat_id ORDER BY h.seat_id) FILTER (WHERE h.seat_id IS NOT NULL), '{}') AS seat_ids
FROM orders o
LEFT JOIN order_items oi ON oi.order_id = o.id
LEFT JOIN holds h ON h.id = oi.hold_id
WHERE o.status = 'paid'
  AND NOT EXISTS (SELECT 1 FROM notifications n WHERE n.kind = 'tickets_issued' AND n.payload->>'order_id' = o.id::text)
GROUP BY o.id;
INSERT INTO outbox (id, aggregatetype, aggregateid, type, payload)
SELECT id, 'order', order_id::text, 'OrderPaid',
       jsonb_build_object('id', id, 'order_id', order_id, 'event_id', event_id, 'user_id', user_id,
                          'seat_ids', to_jsonb(seat_ids), 'amount_minor', amount_minor, 'at', now())
FROM repair;
SELECT count(*) AS events_for_repair FROM repair;
DELETE FROM outbox WHERE id IN (SELECT id FROM repair);
COMMIT;
