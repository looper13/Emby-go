//go:build !embedui

package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

func loadWebUIAssets() fs.FS {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return webUIDevFS("")
	}
	return webUIDevFS(filepath.Join(filepath.Dir(source), "web_dist"))
}

type webUIDevFS string

func (files webUIDevFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	// Open lazily: API-only builds and startup do not require web_dist to exist.
	// Root also prevents a development symlink from escaping the asset directory.
	root, err := os.OpenRoot(string(files))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.FS().Open(name)
}
