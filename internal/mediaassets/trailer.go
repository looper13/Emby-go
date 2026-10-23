package mediaassets

import (
	"os"
	"path/filepath"
	"strings"
)

// TrailerPath is tied to the full source stem, preserving version and CD suffixes.
func TrailerPath(source, directory string) string {
	if strings.TrimSpace(directory) == "" {
		directory = filepath.Dir(source)
	}
	stem := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	return filepath.Join(directory, stem+"-trailer.mp4")
}

func LocalTrailer(source, directory string) string {
	path := TrailerPath(source, directory)
	if stat, err := os.Stat(path); err == nil && stat.Mode().IsRegular() && stat.Size() > 0 {
		return path
	}
	return ""
}
