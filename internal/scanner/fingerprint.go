package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type fileStamp struct {
	Path     string
	Size     int64
	Modified int64
	Mode     os.FileMode
	Missing  bool
}

type metadataState struct{ Fingerprint string }

// Source files are enumerated for membership only. A metadata fingerprint
// depends exclusively on the primary and CD fallback NFO, including absence.
var metadataStat = os.Stat

func readMetadataState(source, fallbackNFO string) (metadataState, error) {
	primary := strings.TrimSuffix(source, filepath.Ext(source)) + ".nfo"
	paths := []string{primary}
	if fallbackNFO != "" && fallbackNFO != primary {
		paths = append(paths, fallbackNFO)
	}
	sort.Strings(paths)
	stamps := make([]fileStamp, 0, len(paths))
	for _, path := range paths {
		stamp := fileStamp{Path: path}
		info, err := metadataStat(path)
		if os.IsNotExist(err) {
			stamp.Missing = true
		} else if err != nil {
			return metadataState{}, err
		} else {
			stamp.Size, stamp.Modified, stamp.Mode = info.Size(), info.ModTime().UnixNano(), info.Mode()
		}
		stamps = append(stamps, stamp)
	}
	payload, err := json.Marshal(stamps)
	if err != nil {
		return metadataState{}, err
	}
	digest := sha256.Sum256(payload)
	return metadataState{Fingerprint: "nfo-v3:" + hex.EncodeToString(digest[:])}, nil
}
