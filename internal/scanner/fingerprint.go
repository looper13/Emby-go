package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"emby-go/internal/imageutil"
)

type fileStamp struct {
	Path     string
	Size     int64
	Modified int64
	Mode     os.FileMode
	Missing  bool
}

type sourceState struct {
	Fingerprint string
	Images      imageutil.ImagePaths
	Info        os.FileInfo
}

func readSourceState(path string, parts []string, fallbackNFO string, directory *imageDirectory) (sourceState, error) {
	state := sourceState{}
	if err := directory.load(filepath.Dir(path)); err != nil {
		return state, err
	}
	imageInfo := make(map[string]os.FileInfo)
	state.Images = imageutil.FindImages(filepath.Dir(path), func(image string) bool {
		if !directory.contains(image) {
			return false
		}
		info, err := os.Stat(image)
		if err == nil {
			imageInfo[image] = info
		}
		return err == nil
	})
	dependencies := []string{path}
	dependencies = append(dependencies, parts...)
	optional := map[string]bool{}
	nfoPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".nfo"
	dependencies = append(dependencies, nfoPath)
	optional[nfoPath] = true
	if fallbackNFO != "" && fallbackNFO != nfoPath {
		dependencies = append(dependencies, fallbackNFO)
		optional[fallbackNFO] = true
	}
	for _, image := range []string{state.Images.Poster, state.Images.Backdrop, state.Images.Landscape} {
		if image != "" {
			dependencies = append(dependencies, image)
		}
	}
	sort.Strings(dependencies)
	stamps := make([]fileStamp, 0, len(dependencies))
	for _, dependency := range dependencies {
		stamp := fileStamp{Path: dependency}
		info, cached := imageInfo[dependency]
		var err error
		if !cached {
			info, err = os.Stat(dependency)
		}
		if os.IsNotExist(err) && optional[dependency] {
			stamp.Missing = true
		} else if err != nil {
			return state, err
		} else {
			stamp.Size, stamp.Modified, stamp.Mode = info.Size(), info.ModTime().UnixNano(), info.Mode()
			if dependency == path {
				state.Info = info
			}
		}
		stamps = append(stamps, stamp)
	}
	payload, err := json.Marshal(stamps)
	if err != nil {
		return state, err
	}
	digest := sha256.Sum256(payload)
	state.Fingerprint = "v1:" + hex.EncodeToString(digest[:])
	return state, nil
}
