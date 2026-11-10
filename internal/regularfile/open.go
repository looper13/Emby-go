// Package regularfile opens local documents without reading special-file streams.
package regularfile

import (
	"errors"
	"os"
)

var ErrNotRegular = errors.New("not a regular file")

// Open follows file symlinks, as the document readers historically did. It does
// not confine paths to a library root or impose a filesystem I/O deadline.
func Open(path string) (*os.File, error) {
	before, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, &os.PathError{Op: "open", Path: path, Err: ErrNotRegular}
	}
	file, err := os.OpenFile(path, os.O_RDONLY|nonblock, 0)
	if err != nil {
		return nil, err
	}
	actual, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !actual.Mode().IsRegular() {
		file.Close()
		return nil, &os.PathError{Op: "open", Path: path, Err: ErrNotRegular}
	}
	return file, nil
}
