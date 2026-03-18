package webassets

import "embed"

// StaticFS embeds the dashboard static assets for binary and container packaging.
//
//go:embed static
var StaticFS embed.FS
