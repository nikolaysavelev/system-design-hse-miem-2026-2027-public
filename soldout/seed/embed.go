// Package seed встраивает seed.sql — тестовые данные (3 мероприятия × 40 000 мест).
package seed

import "embed"

// FS — seed-скрипты.
//
//go:embed seed.sql
var FS embed.FS
