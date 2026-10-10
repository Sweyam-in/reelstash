//go:build unix

package diskinfo

import "syscall"

func statfs(path string) (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Usage{}, err
	}
	total := int64(st.Blocks) * int64(st.Bsize)
	available := int64(st.Bavail) * int64(st.Bsize)
	return Usage{Total: total, Used: total - int64(st.Bfree)*int64(st.Bsize), Available: available}, nil
}
