package librarywatch

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

func TestWindowsNotificationDecoding(t *testing.T) {
	root := t.TempDir()
	name := utf16.Encode([]rune("电影\\测试.strm"))
	data := make([]byte, 12+len(name)*2)
	binary.LittleEndian.PutUint32(data[4:], windows.FILE_ACTION_RENAMED_NEW_NAME)
	binary.LittleEndian.PutUint32(data[8:], uint32(len(name)*2))
	for index, character := range name {
		binary.LittleEndian.PutUint16(data[12+index*2:], character)
	}
	events, err := windowsEvents(root, data)
	if err != nil || len(events) != 1 || events[0].Op != fsnotify.Create || events[0].Name != filepath.Join(root, "电影", "测试.strm") {
		t.Fatalf("unicode rename notification = %+v, %v", events, err)
	}
	for _, invalid := range [][]byte{nil, data[:5], data[:len(data)-1]} {
		if _, err := windowsEvents(root, invalid); !errors.Is(err, fsnotify.ErrEventOverflow) {
			t.Fatalf("truncated notification should trigger reconciliation: %v", err)
		}
	}
	binary.LittleEndian.PutUint32(data, 1)
	if _, err := windowsEvents(root, data); !errors.Is(err, fsnotify.ErrEventOverflow) {
		t.Fatalf("invalid next offset should trigger reconciliation: %v", err)
	}
}
