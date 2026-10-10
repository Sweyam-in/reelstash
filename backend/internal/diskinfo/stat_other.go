//go:build !unix

package diskinfo

import "errors"

func statfs(path string) (Usage, error) {
	return Usage{}, errors.New("disk usage is not supported on this platform")
}
