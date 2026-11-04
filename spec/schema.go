// Package spec embeds the exact development schemas used by Go and TypeScript.
package spec

import "embed"

//go:embed schemas/*.schema.json
var Schemas embed.FS

// Crypto contains pinned verifier material, never voter-supplied artifacts.
//
//go:embed crypto/*.json
var Crypto embed.FS
