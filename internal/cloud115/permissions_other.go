//go:build !windows

package cloud115

import "os"

func privateFile(filename string) error { return os.Chmod(filename, 0600) }
