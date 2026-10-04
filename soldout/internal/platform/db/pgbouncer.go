package db

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PgBouncerVersion возвращает версию PgBouncer, если url ведёт на него (порт 6432 / хост pgbouncer);
// иначе "direct". Используется в диагностике /readyz: на занятии 2 важно, что версия ≥ 1.21
// (поддержка prepared statements в transaction pooling).
func PgBouncerVersion(ctx context.Context, dbURL string) string {
	u, err := url.Parse(dbURL)
	if err != nil || !(strings.Contains(u.Host, "pgbouncer") || strings.HasSuffix(u.Host, ":6432")) {
		return "direct"
	}
	admin := *u
	admin.Path = "/pgbouncer"
	q := admin.Query()
	q.Set("default_query_exec_mode", "simple_protocol") // консоль PgBouncer не понимает extended protocol
	admin.RawQuery = q.Encode()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, admin.String())
	if err != nil {
		return "недоступна консоль: " + err.Error()
	}
	defer conn.Close(ctx)
	var version string
	if err := conn.QueryRow(ctx, "SHOW VERSION").Scan(&version); err != nil {
		return "недоступна консоль: " + err.Error()
	}
	return version
}
