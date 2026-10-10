//go:build embedui

package server

import (
	"embed"
	"io/fs"
)

//go:embed web_dist
var embeddedWebUI embed.FS

func loadWebUIAssets() fs.FS {
	files, err := fs.Sub(embeddedWebUI, "web_dist")
	if err != nil {
		panic(err)
	}
	return files
}
