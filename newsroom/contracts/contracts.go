// Package contracts embeds the same artifact contracts published to PolyMaths.
package contracts

import "embed"

//go:embed *.schema.json
var FS embed.FS
