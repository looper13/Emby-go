package nfo

import (
	"errors"
	"io"

	"emby-go/internal/regularfile"
)

const MaxFileSize = 8 << 20

var ErrTooLarge = errors.New("nfo: 文件超过 8 MiB 上限")

// ReadFile bounds allocation even if a document grows while being read. All NFO
// read/modify/write paths share this boundary and retain their existing locks.
func ReadFile(path string) ([]byte, error) {
	file, err := regularfile.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileSize {
		return nil, ErrTooLarge
	}
	return data, nil
}
