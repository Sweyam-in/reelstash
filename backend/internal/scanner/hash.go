package scanner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

// fullHashTimeout bounds FullHash so a stalled network mount or a
// pathologically slow disk can't wedge an entire scan forever. Go's plain
// file reads aren't context-cancellable at the syscall level, so this runs
// the read on its own goroutine and simply stops waiting on it — the
// goroutine itself may still be blocked afterward, but the scan moves on.
const fullHashTimeout = 5 * time.Minute

const partialHashSampleBytes = 1 << 20 // 1 MiB from each end

// partialHashTimeout bounds PartialHash the same way fullHashTimeout bounds
// FullHash: it only samples 2 MiB, but a stalled network mount can still
// hang the very first read forever without one.
const partialHashTimeout = 30 * time.Second

// runWithTimeout runs fn on its own goroutine and gives up waiting for it
// after timeout. Plain file I/O isn't context-cancellable at the syscall
// level, so a timed-out fn may still be blocked in the background
// afterward — the caller moves on regardless, accepting that leak in
// exchange for the scan/job never hanging forever on one bad file or mount.
func runWithTimeout[T any](timeout time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		val T
		err error
	}
	done := make(chan result, 1)
	go func() {
		v, err := fn()
		done <- result{val: v, err: err}
	}()
	select {
	case r := <-done:
		return r.val, r.err
	case <-time.After(timeout):
		var zero T
		return zero, fmt.Errorf("timed out after %s", timeout)
	}
}

// statWithTimeout wraps os.Stat so a stalled/stale mount can't block a scan
// indefinitely just from checking whether a path exists.
func statWithTimeout(path string, timeout time.Duration) (os.FileInfo, error) {
	info, err := runWithTimeout(timeout, func() (os.FileInfo, error) { return os.Stat(path) })
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	return info, nil
}

// PartialHash hashes the first and last partialHashSampleBytes of a file
// (or the whole file if smaller). A library scan uses it, with file size,
// to match a file to an existing title. Exact duplicate detection uses
// FullHash instead, and only when an admin starts that scan.
func PartialHash(path string, size int64) (string, error) {
	return runWithTimeout(partialHashTimeout, func() (string, error) {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()

		h := sha256.New()
		buf := make([]byte, partialHashSampleBytes)

		n, err := io.ReadFull(f, buf)
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return "", err
		}
		h.Write(buf[:n])

		if size > partialHashSampleBytes {
			if _, err := f.Seek(-partialHashSampleBytes, io.SeekEnd); err == nil {
				tail := make([]byte, partialHashSampleBytes)
				n, err := io.ReadFull(f, tail)
				if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
					return "", err
				}
				h.Write(tail[:n])
			}
		}

		return hex.EncodeToString(h.Sum(nil)), nil
	})
}

// FullHash streams the entire file through SHA-256 for exact-duplicate
// detection. It gives up (returning an error) after fullHashTimeout rather
// than blocking forever on a stalled read.
func FullHash(path string) (string, error) {
	sum, err := runWithTimeout(fullHashTimeout, func() (string, error) {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	})
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return sum, nil
}
