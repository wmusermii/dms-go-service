// Package migrations embeds the SQL migration files so the binary can apply
// them on start-up (see internal/database.Migrate).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
