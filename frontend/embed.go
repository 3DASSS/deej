// Package frontend embeds the built settings UI assets.
package frontend

import "embed"

// Dist holds the compiled frontend assets (dist is populated by "npm run build")
//
//go:embed all:dist
var Dist embed.FS
