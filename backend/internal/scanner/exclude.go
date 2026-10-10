package scanner

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/pathguard"
)

// Excluder decides which paths a scan skips, from the admin's scan rules.
// Patterns follow .gitignore conventions:
//   - no slash ("@eaDir", "*.part", "Samples"): matches a file or folder name
//     anywhere;
//   - with a slash ("Movies/Old", "Clips/**/tmp"): matches a path from the
//     library folder (or the disk root, for files outside every library). A
//     leading "/" is optional.
//
// "*" and "?" stay within one path segment; "**" spans folders. Excluding a
// folder excludes everything under it.
type Excluder struct {
	names []*regexp.Regexp
	paths []*regexp.Regexp
}

// NewExcluder compiles patterns; it fails on the first invalid one.
func NewExcluder(patterns []string) (*Excluder, error) {
	e := &Excluder{}
	for _, raw := range patterns {
		p := strings.Trim(strings.TrimSpace(raw), "/")
		if p == "" {
			continue
		}
		re, err := regexp.Compile("^" + globToRegexp(p) + "$")
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", raw, err)
		}
		if strings.Contains(p, "/") {
			e.paths = append(e.paths, re)
		} else {
			e.names = append(e.names, re)
		}
	}
	return e, nil
}

// ValidatePattern reports whether a scan rule pattern is usable.
func ValidatePattern(pattern string) error {
	if strings.Trim(strings.TrimSpace(pattern), "/") == "" {
		return fmt.Errorf("pattern is empty")
	}
	_, err := NewExcluder([]string{pattern})
	return err
}

// Empty reports whether there are no rules.
func (e *Excluder) Empty() bool { return e == nil || len(e.names)+len(e.paths) == 0 }

// Excluded reports whether relPath (slash-separated, relative to the disk
// root) or any folder above it matches a rule.
func (e *Excluder) Excluded(relPath string) bool {
	if e.Empty() {
		return false
	}
	relPath = strings.Trim(relPath, "/")
	if relPath == "" {
		return false
	}
	segments := strings.Split(relPath, "/")
	for _, seg := range segments {
		for _, re := range e.names {
			if re.MatchString(seg) {
				return true
			}
		}
	}
	for i := range segments {
		prefix := strings.Join(segments[:i+1], "/")
		for _, re := range e.paths {
			if re.MatchString(prefix) {
				return true
			}
		}
	}
	return false
}

func globToRegexp(glob string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*' && i+1 < len(glob) && glob[i+1] == '*':
			i++
			if i+1 < len(glob) && glob[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?") // "**/" matches zero or more folders
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}

// RuleSet holds the scan rules for one storage location. A path inside a
// library folder is checked against the rules for every library plus that
// library's own, relative to the folder; a path outside every library only
// against the rules for every library, relative to the disk root.
type RuleSet struct {
	global *Excluder
	roots  []ruleRoot // longest path first, so the innermost folder wins
	// locked are folders (relative to the disk root) no rule can let in,
	// such as the downloads in-progress folder; see pathguard.
	locked []string
}

// Lock adds folders (relative to the disk root, "" = all of it) that are
// always skipped, whatever the rules say.
func (rs *RuleSet) Lock(rels ...string) {
	for _, r := range rels {
		rs.locked = append(rs.locked, strings.Trim(r, "/"))
	}
}

type ruleRoot struct {
	path string
	ex   *Excluder
}

// Excluded reports whether relPath (relative to the disk root) is skipped.
func (rs *RuleSet) Excluded(relPath string) bool {
	if rs == nil {
		return false
	}
	relPath = strings.Trim(relPath, "/")
	for _, l := range rs.locked {
		if l == "" || relPath == l || strings.HasPrefix(relPath, l+"/") {
			return true
		}
	}
	for _, root := range rs.roots {
		switch {
		case root.path == "":
			return root.ex.Excluded(relPath)
		case relPath == root.path:
			return false // the library folder itself
		case strings.HasPrefix(relPath, root.path+"/"):
			return root.ex.Excluded(relPath[len(root.path)+1:])
		}
	}
	return rs.global.Excluded(relPath)
}

// NewRuleSet builds a rule set from the rules for every library, each
// library's own rules and the library folders on the disk (folder path ->
// library id).
func NewRuleSet(global []string, byLibrary map[int64][]string, roots map[string]int64) (*RuleSet, error) {
	g, err := NewExcluder(global)
	if err != nil {
		return nil, err
	}
	rs := &RuleSet{global: g}
	for path, libraryID := range roots {
		ex, err := NewExcluder(append(append([]string{}, global...), byLibrary[libraryID]...))
		if err != nil {
			return nil, err
		}
		rs.roots = append(rs.roots, ruleRoot{path: path, ex: ex})
	}
	sort.Slice(rs.roots, func(i, j int) bool { return len(rs.roots[i].path) > len(rs.roots[j].path) })
	return rs, nil
}

// LoadRules builds the rule set for one storage location from the scan rules
// and the library folders on it.
func LoadRules(ctx context.Context, db *pgxpool.Pool, storageLocationID int64) (*RuleSet, error) {
	rows, err := db.Query(ctx, `SELECT library_id, pattern FROM scan_rule`)
	if err != nil {
		return nil, err
	}
	var global []string
	byLibrary := map[int64][]string{}
	for rows.Next() {
		var libraryID *int64
		var p string
		if err := rows.Scan(&libraryID, &p); err != nil {
			rows.Close()
			return nil, err
		}
		if libraryID == nil {
			global = append(global, p)
		} else {
			byLibrary[*libraryID] = append(byLibrary[*libraryID], p)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rootRows, err := db.Query(ctx, `SELECT path, library_id FROM library_root WHERE storage_location_id = $1`, storageLocationID)
	if err != nil {
		return nil, err
	}
	roots := map[string]int64{}
	for rootRows.Next() {
		var path string
		var libraryID int64
		if err := rootRows.Scan(&path, &libraryID); err != nil {
			rootRows.Close()
			return nil, err
		}
		roots[path] = libraryID
	}
	rootRows.Close()
	if err := rootRows.Err(); err != nil {
		return nil, err
	}
	rs, err := NewRuleSet(global, byLibrary, roots)
	if err != nil {
		return nil, err
	}
	var mount string
	if err := db.QueryRow(ctx, `SELECT mount_path FROM storage_location WHERE id = $1`, storageLocationID).Scan(&mount); err == nil {
		rs.Lock(pathguard.Load(ctx, db).LockedRel(mount)...)
	}
	return rs, nil
}
