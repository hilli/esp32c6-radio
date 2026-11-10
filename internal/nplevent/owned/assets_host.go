//go:build !tinygo && c6_owned_entry

package owned

import "embed"

//go:embed runtime/*
var _ embed.FS
