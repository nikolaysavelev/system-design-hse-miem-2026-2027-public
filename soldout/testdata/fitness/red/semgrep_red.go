// КРАСНЫЙ ПРИМЕР (semgrep): каждое правило .semgrep.yml должно сработать здесь хотя бы раз.
package red

import (
	"context"
	"database/sql"
	"time"
)

// booking-session-advisory-lock: сессионный advisory lock несовместим с transaction pooling PgBouncer.
const lockSQL = "SELECT pg_advisory_lock($1)"
const unlockSQL = "SELECT pg_advisory_unlock($1)"

// no-select-star
const seatsSQL = "SELECT * FROM seats WHERE id = $1"

// no-context-background-outside-cmd
func handler(db *sql.DB) {
	_, _ = db.ExecContext(context.Background(), lockSQL, 1)
}

// no-time-now-in-domain (файл считается доменным по пути */domain/* — здесь имитация для примера)
func expiresAt() time.Time { return time.Now().Add(10 * time.Minute) }
