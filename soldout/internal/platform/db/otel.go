package db

import (
	"context"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// maxStatementAttr — db.statement в span'е обрезается: полный SQL раздувает трейсы и может нести данные.
const maxStatementAttr = 200

// otelTracer — span на каждый SQL-запрос (otelpgx) с именем из комментария /* booking.insert_hold */,
// тем же, что у метрики db_query_duration_seconds и в pg_stat_statements; ожидание соединения — span pgx.acquire.
// Span'ы создаются только внутри трейса запроса: фоновый expirer без родителя не пишет ничего.
type otelTracer struct{ *otelpgx.Tracer }

func newOTelTracer() otelTracer {
	return otelTracer{otelpgx.NewTracer(
		otelpgx.WithSpanNameFunc(spanName),
		otelpgx.WithDisableSQLStatementInAttributes(),
		otelpgx.WithDisableConnectionDetailsInAttributes(),
	)}
}

func spanName(sql string) string {
	if m := queryNameRe.FindStringSubmatch(sql); m != nil {
		return m[1]
	}
	// BEGIN/COMMIT/ROLLBACK и запросы без имени — по первому слову
	f := strings.Fields(sql)
	if len(f) == 0 {
		return "sql"
	}
	return "sql." + strings.ToLower(f[0])
}

func (t otelTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		stmt := data.SQL
		if len(stmt) > maxStatementAttr {
			stmt = stmt[:maxStatementAttr]
		}
		span.SetAttributes(attribute.String("db.statement", stmt))
	}
	return ctx
}

func (t otelTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	parent := trace.SpanFromContext(ctx).SpanContext().SpanID()
	ctx = t.Tracer.TraceAcquireStart(ctx, pool, data)
	if span := trace.SpanFromContext(ctx); span.IsRecording() && span.SpanContext().SpanID() != parent {
		span.SetName("pgx.acquire")
	}
	return ctx
}

// TracePrepareStart/End — без span'ов: pgx готовит statement внутри запроса, и prepare дублировал бы
// каждый SQL-span дочерним с тем же именем. Время prepare остаётся внутри span'а запроса.
func (t otelTracer) TracePrepareStart(ctx context.Context, _ *pgx.Conn, _ pgx.TracePrepareStartData) context.Context {
	return ctx
}

func (t otelTracer) TracePrepareEnd(context.Context, *pgx.Conn, pgx.TracePrepareEndData) {}
