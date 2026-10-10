package scanner

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"html"
	"os"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	nfoActorBlock = regexp.MustCompile(`(?s)[ \t]*<actor>.*?</actor>[ \t]*\r?\n?`)
	nfoNameTag    = regexp.MustCompile(`(?s)<name>(.*?)</name>`)
)

func nfoNameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// RewriteNFOPerformer renames every <actor> called one of `from` to `to` and
// drops actors that would then appear twice. Everything else in the file is
// left byte-for-byte alone. It reports whether the file changed.
func RewriteNFOPerformer(path string, from []string, to string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	rename := make(map[string]bool, len(from))
	for _, name := range from {
		rename[nfoNameKey(name)] = true
	}
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(to)); err != nil {
		return false, err
	}

	seen := map[string]bool{}
	out := nfoActorBlock.ReplaceAllStringFunc(string(data), func(block string) string {
		match := nfoNameTag.FindStringSubmatchIndex(block)
		if match == nil {
			return block
		}
		name := html.UnescapeString(block[match[2]:match[3]])
		if rename[nfoNameKey(name)] {
			block = block[:match[2]] + escaped.String() + block[match[3]:]
			name = to
		}
		key := nfoNameKey(name)
		if seen[key] {
			return ""
		}
		seen[key] = true
		return block
	})
	if out == string(data) {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(out), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

// RefreshNFORecord re-reads an NFO that was edited in place and updates the
// stored copy so the next scan sees it as unchanged.
func RefreshNFORecord(ctx context.Context, db *pgxpool.Pool, mediaFileID int64, nfoPath string) error {
	movie, err := parseNFO(nfoPath)
	if err != nil {
		return err
	}
	hash, err := FullHash(nfoPath)
	if err != nil {
		return err
	}
	tag, err := db.Exec(ctx, `
		UPDATE nfo_metadata SET parsed_metadata = $2, content_hash = $3, updated_at = now()
		WHERE media_file_id = $1
	`, mediaFileID, mustJSON(movie), hash)
	if err != nil || tag.RowsAffected() > 0 {
		return err
	}
	// No row yet: an identity-only stub that an edit has just filled in. Record
	// it now it describes the title, but never while it is still bare (a bare
	// stub must not make the title look NFO-described to the auto-matcher).
	if !movie.hasMetadata() {
		return nil
	}
	var rel string
	if err := db.QueryRow(ctx, `SELECT relative_path FROM media_file WHERE id = $1`, mediaFileID).Scan(&rel); err != nil {
		return err
	}
	return recordNFO(ctx, db, mediaFileID, rel, movie, hash)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// RemoveNFOPerformer drops every <actor> called `name`. It reports whether the file changed.
func RemoveNFOPerformer(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	out := nfoActorBlock.ReplaceAllStringFunc(string(data), func(block string) string {
		if m := nfoNameTag.FindStringSubmatch(block); m != nil && nfoNameKey(html.UnescapeString(m[1])) == nfoNameKey(name) {
			return ""
		}
		return block
	})
	return writeIfChanged(path, string(data), out)
}

// AddNFOPerformer appends an <actor> to the NFO unless one with that name exists.
func AddNFOPerformer(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, block := range nfoActorBlock.FindAllString(string(data), -1) {
		if m := nfoNameTag.FindStringSubmatch(block); m != nil && nfoNameKey(html.UnescapeString(m[1])) == nfoNameKey(name) {
			return false, nil
		}
	}
	var escaped strings.Builder
	if err := xml.EscapeText(&escaped, []byte(name)); err != nil {
		return false, err
	}
	end := strings.LastIndex(string(data), "</movie>")
	if end < 0 {
		return false, nil
	}
	out := string(data[:end]) + "  <actor><name>" + escaped.String() + "</name></actor>\n" + string(data[end:])
	return writeIfChanged(path, string(data), out)
}

func writeIfChanged(path, before, after string) (bool, error) {
	if before == after {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(after), info.Mode().Perm())
}
