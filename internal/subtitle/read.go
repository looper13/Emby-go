package subtitle

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReadLocal(mediaPath, path string) ([]byte, error) {
	resolved, err := ResolveLocal(mediaPath, path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxFileSize {
		return nil, errors.New("字幕不是可读取的普通文件或超过 16 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if len(data) > MaxFileSize {
		return nil, errors.New("字幕文件超过 16 MiB")
	}
	return data, err
}

func ResolveLocal(mediaPath, path string) (string, error) {
	dir, err := filepath.EvalSymlinks(filepath.Dir(mediaPath))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(dir, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", errors.New("文件不在媒体目录内")
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("文件不是可读取的普通文件")
	}
	return resolved, nil
}
