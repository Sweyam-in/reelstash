// Package diskinfo reads capacity and mount details for a storage path,
// with timeouts so a stalled network mount can't hang the caller.
package diskinfo

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Usage is a filesystem's capacity in bytes.
type Usage struct {
	Total     int64 `json:"total"`
	Used      int64 `json:"used"`
	Available int64 `json:"available"`
}

// Mount describes the filesystem a path lives on. Fields are empty when the
// platform can't tell.
type Mount struct {
	MountPoint string `json:"mountPoint,omitempty"`
	Device     string `json:"device,omitempty"`
	FSType     string `json:"fsType,omitempty"`
	ReadOnly   bool   `json:"readOnly"`
}

// DefaultTimeout bounds one filesystem call against a possibly stalled mount.
const DefaultTimeout = 10 * time.Second

func withTimeout[T any](timeout time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.val, r.err
	case <-time.After(timeout):
		var zero T
		return zero, fmt.Errorf("timed out after %s", timeout)
	}
}

// Stat returns the capacity of the filesystem holding path.
func Stat(path string, timeout time.Duration) (Usage, error) {
	return withTimeout(timeout, func() (Usage, error) { return statfs(path) })
}

// MountOf describes the filesystem holding path.
func MountOf(path string, timeout time.Duration) (Mount, error) {
	return withTimeout(timeout, func() (Mount, error) { return mountOf(path) })
}

// Network reports whether fsType is a share another machine can write to
// without this host's file watcher seeing it.
func Network(fsType string) bool {
	s := strings.ToLower(strings.TrimSpace(fsType))
	switch s {
	case "nfs", "nfs4", "cifs", "smb", "smb2", "smb3", "smbfs", "afpfs", "webdav", "9p":
		return true
	}
	return strings.Contains(s, "nfs") || strings.Contains(s, "smb") || strings.Contains(s, "cifs") || strings.Contains(s, "sshfs")
}

// HasEntries reports whether the directory holds anything. An unmounted disk
// usually leaves an empty mount point behind.
func HasEntries(path string, timeout time.Duration) (bool, error) {
	return withTimeout(timeout, func() (bool, error) {
		f, err := os.Open(path)
		if err != nil {
			return false, err
		}
		defer f.Close()
		names, err := f.Readdirnames(1)
		if err != nil && len(names) == 0 {
			return false, nil
		}
		return len(names) > 0, nil
	})
}
