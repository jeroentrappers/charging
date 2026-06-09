// Package migrations embeds the goose SQL migrations so they can be applied
// from a compiled binary (no goose CLI or source tree needed at runtime).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
