// Package migrations встраивает SQL-миграции в бинарник. Правило: схема меняется только новым файлом
// NNNN_name.up.sql / .down.sql; существующие файлы не редактируются.
package migrations

import "embed"

// FS — файлы миграций.
//
//go:embed *.sql
var FS embed.FS
