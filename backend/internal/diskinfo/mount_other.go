//go:build !linux && !darwin

package diskinfo

func mountOf(path string) (Mount, error) { return Mount{}, nil }
