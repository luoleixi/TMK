//go:build adminui

package adminui

import "embed"

//go:embed dist/*
var assets embed.FS

const assetRoot = "dist"
