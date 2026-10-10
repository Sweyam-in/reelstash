package diskinfo

import "syscall"

// mntReadOnly is MNT_RDONLY from <sys/mount.h>; the syscall package does not export it on darwin.
const mntReadOnly = 0x1

func mountOf(path string) (Mount, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Mount{}, err
	}
	return Mount{
		MountPoint: cString(st.Mntonname[:]),
		Device:     cString(st.Mntfromname[:]),
		FSType:     cString(st.Fstypename[:]),
		ReadOnly:   st.Flags&mntReadOnly != 0,
	}, nil
}

func cString(in []int8) string {
	out := make([]byte, 0, len(in))
	for _, c := range in {
		if c == 0 {
			break
		}
		out = append(out, byte(c))
	}
	return string(out)
}
