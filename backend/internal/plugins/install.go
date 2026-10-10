package plugins

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	maxManifest   = 64 << 10
	maxBundleSize = 200 << 20 // all files together, once unpacked
	maxBundleFile = 5000
	// MaxArchive is the largest .zip accepted.
	MaxArchive = 64 << 20
)

// openZip reads a bundle archive held in memory.
func openZip(data []byte) (fs.FS, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("not a valid .zip bundle: %w", err)
	}
	// Reject, rather than silently skip, anything that points outside the bundle.
	for _, f := range zr.File {
		if name := strings.TrimSuffix(f.Name, "/"); !fs.ValidPath(name) || strings.Contains(name, `\`) {
			return nil, fmt.Errorf("the bundle contains an unsafe path: %q", f.Name)
		}
	}
	return zr, nil
}

// bundleRoot finds plugin.json at the top of fsys, or inside the one folder
// that holds everything (what zipping a folder produces), and returns the fs
// rooted there with the parsed manifest.
func bundleRoot(fsys fs.FS) (fs.FS, Manifest, error) {
	root := fsys
	raw, err := readLimited(fsys, ManifestFile, maxManifest)
	if errors.Is(err, fs.ErrNotExist) {
		entries, derr := fs.ReadDir(fsys, ".")
		if derr == nil && len(entries) == 1 && entries[0].IsDir() {
			sub, serr := fs.Sub(fsys, entries[0].Name())
			if serr == nil {
				root = sub
				raw, err = readLimited(sub, ManifestFile, maxManifest)
			}
		}
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Manifest{}, errors.New("the bundle has no plugin.json at its top level")
		}
		return nil, Manifest{}, err
	}
	m, err := ParseManifest(raw)
	if err != nil {
		return nil, Manifest{}, err
	}
	return root, m, nil
}

func readLimited(fsys fs.FS, name string, limit int64) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%s is too large", name)
	}
	return raw, nil
}

// extract writes the bundle's files under dst. Only regular files and folders
// are accepted (links could point outside the bundle), and the total size and
// count are capped so an archive cannot fill the disk.
func extract(src fs.FS, dst string) error {
	var total int64
	var files int
	return fs.WalkDir(src, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name == "." {
			return os.MkdirAll(dst, 0o755)
		}
		target := filepath.Join(dst, filepath.FromSlash(name))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: links and special files are not allowed in a bundle", name)
		}
		if files++; files > maxBundleFile {
			return fmt.Errorf("the bundle has more than %d files", maxBundleFile)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		perm := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			perm = 0o755
		}
		if err := os.MkdirAll(path.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := src.Open(name)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(in, maxBundleSize-total+1))
		cerr := out.Close()
		total += n
		if total > maxBundleSize {
			return fmt.Errorf("the bundle is larger than %d MB unpacked", maxBundleSize>>20)
		}
		if err != nil {
			return err
		}
		return cerr
	})
}
