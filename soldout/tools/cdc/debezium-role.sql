-- Роль Debezium: REPLICATION (слот логической репликации) и SELECT на outbox. Идемпотентно, выполняет tools/cdc/register.sh
-- после миграций (initdb не подходит: при make switch том базы уже существует, а outbox ещё нет).
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'debezium') THEN
        CREATE ROLE debezium WITH LOGIN REPLICATION PASSWORD 'debezium';
    END IF;
END
$$;
GRANT CONNECT ON DATABASE soldout TO debezium;
GRANT USAGE ON SCHEMA public TO debezium;
GRANT SELECT ON outbox TO debezium;
