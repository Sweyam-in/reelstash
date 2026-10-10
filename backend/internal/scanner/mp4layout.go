package scanner

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ScatteredDataBoxes is the most top-level 'mdat' boxes an MP4 may have
// before it counts as scattered. A normal file has one; some downloaders
// write one per few seconds of video (over a thousand for a feature). A
// browser opening such a file seeks to every box header before it plays,
// which on a byte-range stream pulls almost the whole file first.
const ScatteredDataBoxes = 8

// dataBoxCountLimit stops counting once a file is clearly scattered, so a
// scan costs a handful of header reads per file, not one per box.
const dataBoxCountLimit = 64

// IsMP4Ext reports whether a file extension (".mp4" or "mp4") is an
// ISO-BMFF container whose box layout can be checked.
func IsMP4Ext(ext string) bool {
	switch strings.TrimPrefix(strings.ToLower(ext), ".") {
	case "mp4", "m4v", "mov":
		return true
	}
	return false
}

// CountMP4DataBoxes walks the top-level boxes of an MP4/MOV file and counts
// the 'mdat' ones, stopping at limit. Only box headers are read.
func CountMP4DataBoxes(path string, limit int) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := st.Size()
	var hdr [16]byte
	count := 0
	for pos := int64(0); pos+8 <= size && count < limit; {
		if _, err := f.ReadAt(hdr[:8], pos); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return count, err
		}
		boxSize := int64(binary.BigEndian.Uint32(hdr[:4]))
		kind := string(hdr[4:8])
		switch boxSize {
		case 0: // runs to the end of the file
			boxSize = size - pos
		case 1: // 64-bit size follows the type
			if _, err := f.ReadAt(hdr[8:16], pos+8); err != nil {
				return count, err
			}
			boxSize = int64(binary.BigEndian.Uint64(hdr[8:16]))
		}
		if boxSize < 8 {
			return count, errors.New("malformed mp4 box")
		}
		if kind == "mdat" {
			count++
		}
		pos += boxSize
	}
	return count, nil
}

// mp4DataBoxes is CountMP4DataBoxes for the scanner: nil for files that are
// not MP4s or could not be read, so the column stays "not checked".
func mp4DataBoxes(path string) *int {
	if !IsMP4Ext(filepath.Ext(path)) {
		return nil
	}
	n, err := CountMP4DataBoxes(path, dataBoxCountLimit)
	if err != nil {
		return nil
	}
	return &n
}
