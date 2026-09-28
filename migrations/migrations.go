package migrations

import "embed"

// Files contains the embedded SQL migration scripts.
//
//go:embed *.sql
var Files embed.FS
