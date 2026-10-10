//go:build unix

package scanner

import (
	"os"
	"syscall"
)

// copyOwner gives path the owner and group of orig, best effort: it needs
// the privileges to do so, and a file owned by the server user still plays.
func copyOwner(path string, orig os.FileInfo) {
	if st, ok := orig.Sys().(*syscall.Stat_t); ok {
		_ = os.Lchown(path, int(st.Uid), int(st.Gid))
	}
}
