//go:build !unix

package scanner

import "os"

func copyOwner(string, os.FileInfo) {}
