#!/usr/bin/env bash
# Топики outbox заранее (идемпотентно): 3 партиции под ключ order_id, DLQ — одна партиция.
set -euo pipefail
COMPOSE=${COMPOSE:-docker compose -f docker-compose.yml -f compose/lesson-02.yml -f compose/lesson-04.yml}
KT="$COMPOSE exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092"
$KT --create --if-not-exists --topic outbox.event.order --partitions 3 --replication-factor 1 >/dev/null
$KT --create --if-not-exists --topic outbox.event.order.dlq --partitions 1 --replication-factor 1 >/dev/null
echo "topics: outbox.event.order (3), outbox.event.order.dlq (1)"
