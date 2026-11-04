// Package spec embeds the exact development schemas used by Go and TypeScript.
package spec

import "embed"

//go:embed schemas/*.schema.json
var Schemas embed.FS
