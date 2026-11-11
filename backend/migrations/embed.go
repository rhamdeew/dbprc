// Package migrations is the embedded goose migration set applied when the database opens.
package migrations

import "embed"

// FS holds the SQL migration files, named <UTC timestamp>_<name>.sql.
//
//go:embed *.sql
var FS embed.FS
