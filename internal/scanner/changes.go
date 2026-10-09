package scanner

import (
	"path/filepath"
	"strings"
)

func groupKey(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if match := cdPartPattern.FindStringSubmatch(base); match != nil {
		return filepath.Join(filepath.Dir(path), strings.ToLower(match[1]))
	}
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".strm"
}

func affectedGroups(files []string) map[string]bool {
	keys := make(map[string]bool)
	for _, path := range files {
		extension := strings.ToLower(filepath.Ext(path))
		switch extension {
		case ".strm":
			keys[groupKey(path)] = true
		case ".nfo":
			base := strings.TrimSuffix(path, filepath.Ext(path))
			keys[groupKey(base+".strm")] = true
			keys[filepath.Join(filepath.Dir(path), strings.ToLower(filepath.Base(base)))] = true
		}
	}
	return keys
}
