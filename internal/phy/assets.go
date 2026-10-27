//go:build !tinygo

package phy

import "embed"

// Keep native assets in ordinary module zips and go mod vendor output.
//
//go:embed native/*
var _ embed.FS
