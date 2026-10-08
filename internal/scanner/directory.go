package scanner

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type imageDirectory struct {
	path  string
	names map[string]bool
}

func (directory *imageDirectory) load(path string) error {
	if directory.path == path && directory.names == nil {
		return nil
	}
	folder, err := os.Open(path)
	if err != nil {
		return err
	}
	defer folder.Close()
	const entryLimit = 64
	entries, err := folder.ReadDir(entryLimit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > entryLimit {
		directory.path, directory.names = path, nil
		return nil
	}
	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		names[strings.ToLower(entry.Name())] = true
	}
	directory.path, directory.names = path, names
	return nil
}

func (directory *imageDirectory) contains(path string) bool {
	return directory.names == nil || directory.names[strings.ToLower(filepath.Base(path))]
}
