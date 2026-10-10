package diskinfo

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// mountOf finds the longest mount point in /proc/self/mounts that contains path.
func mountOf(path string) (Mount, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Mount{}, err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	f, err := os.Open("/proc/self/mounts")
	if err != nil {
		return Mount{}, err
	}
	defer f.Close()
	var best Mount
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		point := unescapeMount(fields[1])
		if !within(abs, point) || len(point) < len(best.MountPoint) {
			continue
		}
		readOnly := false
		for _, opt := range strings.Split(fields[3], ",") {
			if opt == "ro" {
				readOnly = true
			}
		}
		best = Mount{MountPoint: point, Device: unescapeMount(fields[0]), FSType: fields[2], ReadOnly: readOnly}
	}
	return best, sc.Err()
}

func within(path, mountPoint string) bool {
	if mountPoint == "/" {
		return true
	}
	return path == mountPoint || strings.HasPrefix(path, mountPoint+"/")
}

// unescapeMount decodes the octal escapes (\040 for a space) /proc/mounts uses.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, ok := octal(s[i+1 : i+4]); ok {
				b.WriteByte(n)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func octal(s string) (byte, bool) {
	var n int
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, false
		}
		n = n*8 + int(c-'0')
	}
	return byte(n), n < 256
}
