//go:build !adminui

package adminui

import "embed"

//go:embed placeholder/*
var assets embed.FS

const assetRoot = "placeholder"
