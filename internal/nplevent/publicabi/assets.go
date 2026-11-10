//go:build !tinygo

package publicabi

import "embed"

//go:embed nimble_npl.h nimble/*.h header.sha256
var declarations embed.FS
