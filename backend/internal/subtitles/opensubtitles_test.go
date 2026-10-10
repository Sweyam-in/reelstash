package subtitles

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	// All zeros: the hash is just the size.
	zeros := filepath.Join(dir, "zeros")
	os.WriteFile(zeros, make([]byte, 3*hashChunk), 0o644)
	if h, err := FileHash(zeros); err != nil || h != "0000000000030000" {
		t.Errorf("zeros: %s %v", h, err)
	}
	// One word set at each end adds up with the size.
	data := make([]byte, 3*hashChunk)
	binary.LittleEndian.PutUint64(data[0:], 0x10)
	binary.LittleEndian.PutUint64(data[len(data)-8:], 0x200)
	words := filepath.Join(dir, "words")
	os.WriteFile(words, data, 0o644)
	if h, _ := FileHash(words); h != "0000000000030210" {
		t.Errorf("words: %s", h)
	}
	small := filepath.Join(dir, "small")
	os.WriteFile(small, []byte("tiny"), 0o644)
	if _, err := FileHash(small); err == nil {
		t.Error("a file under 64 KiB cannot be hashed")
	}
}

func TestParseLanguages(t *testing.T) {
	if got := ParseLanguages(" EN, fr;fr  pt-br"); !reflect.DeepEqual(got, []string{"en", "fr", "pt-br"}) {
		t.Errorf("got %v", got)
	}
}
