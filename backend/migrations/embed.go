// Package migrations embeds the forward-only SQL migrations so the deployment
// image can apply them without a separate migration toolchain.
package migrations

import "embed"

// FS holds every .sql migration in this directory, in lexical order.
//
//go:embed *.sql
var FS embed.FS
