package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	sqlite3 "github.com/mattn/go-sqlite3"
)

// SQLite is a local, single-process database. The path must be on a local
// filesystem because SQLite's WAL mode does not support network filesystems.
type SQLite struct{ db *sql.DB }

func init() {
	sql.Register("reelstash_sqlite3", &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
		if err := conn.RegisterFunc("gen_random_uuid", func() string { return uuid.NewString() }, false); err != nil {
			return err
		}
		if err := conn.RegisterFunc("similarity", sqliteSimilarity, true); err != nil {
			return err
		}
		if err := conn.RegisterFunc("word_similarity", sqliteWordSimilarity, true); err != nil {
			return err
		}
		return conn.RegisterFunc("regexp_replace", func(input, pattern, replacement, flags string) (string, error) {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", err
			}
			if strings.Contains(flags, "g") {
				return re.ReplaceAllString(input, replacement), nil
			}
			return re.ReplaceAllStringFunc(input, func(match string) string { return replacement }), nil
		}, true)
	}})
}

func sqliteTrigrams(s string) map[string]struct{} {
	runes := []rune("  " + strings.ToLower(strings.TrimSpace(s)) + " ")
	grams := make(map[string]struct{})
	for i := 0; i+3 <= len(runes); i++ {
		grams[string(runes[i:i+3])] = struct{}{}
	}
	return grams
}

func sqliteSimilarity(a, b string) float64 {
	left, right := sqliteTrigrams(a), sqliteTrigrams(b)
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	common := 0
	for gram := range left {
		if _, ok := right[gram]; ok {
			common++
		}
	}
	return 2 * float64(common) / float64(len(left)+len(right))
}

func sqliteWordSimilarity(a, b string) float64 {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a != "" && strings.Contains(b, a) {
		return 1
	}
	best := sqliteSimilarity(a, b)
	for _, word := range strings.Fields(b) {
		if score := sqliteSimilarity(a, word); score > best {
			best = score
		}
	}
	return best
}

var (
	castPattern       = regexp.MustCompile(`::(?:text|integer|int|bigint|smallint|numeric|double precision|float8|boolean|jsonb|json|uuid|date|timestamptz|timestamp)(?:\[\])?`)
	anyPattern        = regexp.MustCompile(`(?i)=\s*ANY\s*\(\s*(\$[0-9]+)\s*\)`)
	anyColumnPattern  = regexp.MustCompile(`(?i)=\s*ANY\s*\(\s*([A-Za-z_][A-Za-z_0-9.]*)\s*\)`)
	allPattern        = regexp.MustCompile(`(?i)<>\s*ALL\s*\(\s*(\$[0-9]+)\s*\)`)
	intervalPattern   = regexp.MustCompile(`(?i)(?:now\(\)|CURRENT_TIMESTAMP)\s*-\s*INTERVAL\s*'(\d+\s+\w+)'`)
	toCharPattern     = regexp.MustCompile(`(?i)to_char\(([^,]+),\s*'([^']+)'\)`)
	extractPattern    = regexp.MustCompile(`(?i)EXTRACT\((EPOCH|HOUR|YEAR) FROM ([^()]+)\)`)
	jsonHasKeyPattern = regexp.MustCompile(`([A-Za-z_][A-Za-z_0-9.]*)\s+\?\s+'([A-Za-z_][A-Za-z_0-9]*)'`)
	bindPattern       = regexp.MustCompile(`\$([0-9]+)`)
)

func sqliteQuery(query string) string {
	query = castPattern.ReplaceAllString(query, "")
	query = jsonHasKeyPattern.ReplaceAllString(query, "json_type($1, '$.$2') IS NOT NULL")
	query = anyPattern.ReplaceAllString(query, " IN (SELECT value FROM json_each($1))")
	query = anyColumnPattern.ReplaceAllString(query, " IN (SELECT value FROM json_each($1))")
	query = allPattern.ReplaceAllString(query, " NOT IN (SELECT value FROM json_each($1))")
	query = intervalPattern.ReplaceAllString(query, "datetime('now', '-$1')")
	query = toCharPattern.ReplaceAllStringFunc(query, func(s string) string {
		parts := toCharPattern.FindStringSubmatch(s)
		value := strings.TrimSpace(strings.ReplaceAll(parts[1], " AT TIME ZONE 'UTC'", ""))
		format := parts[2]
		format = strings.ReplaceAll(format, `YYYY-MM-DD"T"HH24:MI:SS"Z"`, `%Y-%m-%dT%H:%M:%SZ`)
		format = strings.ReplaceAll(format, "YYYY-MM-DD HH24:MI", "%Y-%m-%d %H:%M")
		format = strings.ReplaceAll(format, "YYYY-MM-DD", "%Y-%m-%d")
		return "strftime('" + format + "', " + value + ")"
	})
	query = extractPattern.ReplaceAllStringFunc(query, func(s string) string {
		parts := extractPattern.FindStringSubmatch(s)
		value := strings.TrimSpace(parts[2])
		switch strings.ToUpper(parts[1]) {
		case "EPOCH":
			return "unixepoch(" + value + ")"
		case "HOUR":
			return "CAST(strftime('%H', " + value + ") AS INTEGER)"
		default:
			return "CAST(strftime('%Y', " + value + ") AS INTEGER)"
		}
	})
	query = strings.ReplaceAll(query, "EXTRACT(EPOCH FROM (COALESCE(bj.finished_at, now()) - bj.started_at))", "((julianday(COALESCE(bj.finished_at, CURRENT_TIMESTAMP)) - julianday(bj.started_at)) * 86400)")
	query = strings.ReplaceAll(query, "ILIKE", "LIKE")
	query = strings.ReplaceAll(query, "ilike", "LIKE")
	query = strings.ReplaceAll(query, "json_build_object(", "json_object(")
	query = strings.ReplaceAll(query, "json_agg(", "json_group_array(")
	query = strings.ReplaceAll(query, "array_agg(", "json_group_array(")
	query = strings.ReplaceAll(query, "jsonb_object_agg(", "json_group_object(")
	query = strings.ReplaceAll(query, "jsonb_array_length(", "json_array_length(")
	query = strings.ReplaceAll(query, "jsonb_set(payload, '{done}', $2)", "json_set(payload, '$.done', json($2))")
	query = strings.ReplaceAll(query, "cardinality(events)", "json_array_length(events)")
	query = strings.ReplaceAll(query, "library_for_file(mf.storage_location_id, mf.relative_path)", `(SELECT lr.library_id FROM library_root lr WHERE lr.storage_location_id = mf.storage_location_id AND (lr.path = '' OR mf.relative_path = lr.path OR substr(mf.relative_path, 1, length(lr.path) + 1) = lr.path || '/') ORDER BY length(lr.path) DESC, lr.id LIMIT 1)`)
	query = strings.ReplaceAll(query, "now()", "CURRENT_TIMESTAMP")
	query = strings.ReplaceAll(query, "FOR UPDATE OF s", "")
	query = strings.ReplaceAll(query, "FOR UPDATE OF p", "")
	query = strings.ReplaceAll(query, "FOR UPDATE", "")
	query = strings.ReplaceAll(query, "pg_advisory_xact_lock(7283561)", "1")
	query = bindPattern.ReplaceAllString(query, "?$1")
	return query
}

func sqliteArgs(args []any) []any {
	for i, arg := range args {
		switch v := arg.(type) {
		case []string, []int64, []int, []float64:
			encoded, _ := json.Marshal(v)
			args[i] = string(encoded)
		case json.RawMessage:
			args[i] = string(v)
		}
	}
	return args
}

func OpenSQLite(ctx context.Context, path string) (*SQLite, error) {
	if path == "" {
		return nil, errors.New("SQLite path is empty")
	}
	dsn := "file:" + path + "?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL"
	db, err := sql.Open("reelstash_sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	// SQLite permits one writer at a time. A small pool keeps reads responsive
	// while avoiding unbounded write contention from scan workers.
	db.SetMaxOpenConns(4)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping SQLite: %w", err)
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Close()                         { _ = s.db.Close() }
func (s *SQLite) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }
func (s *SQLite) Stat() *pgxpool.Stat            { return nil }

func sqliteTag(query string, result sql.Result) pgconn.CommandTag {
	count, _ := result.RowsAffected()
	fields := strings.Fields(strings.TrimSpace(query))
	command := "EXEC"
	if len(fields) > 0 {
		command = strings.ToUpper(fields[0])
	}
	return pgconn.NewCommandTag(fmt.Sprintf("%s %d", command, count))
}

func (s *SQLite) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	result, err := s.db.ExecContext(ctx, sqliteQuery(query), sqliteArgs(args)...)
	if err != nil {
		log.Printf("SQLite exec failed: %v; SQL: %s", err, query)
		return pgconn.CommandTag{}, err
	}
	return sqliteTag(query, result), nil
}

func (s *SQLite) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	rows, err := s.db.QueryContext(ctx, sqliteQuery(query), sqliteArgs(args)...)
	if err != nil {
		log.Printf("SQLite query failed: %v; SQL: %s", err, query)
		return nil, err
	}
	return &sqliteRows{rows: rows}, nil
}

func (s *SQLite) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return sqliteRow{row: s.db.QueryRowContext(ctx, sqliteQuery(query), sqliteArgs(args)...), query: query}
}

func (s *SQLite) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx}, nil
}

type sqliteRow struct {
	row   *sql.Row
	query string
}

func (r sqliteRow) Scan(dest ...any) error {
	scanDest, decode := sqliteScanDest(dest)
	err := r.row.Scan(scanDest...)
	if errors.Is(err, sql.ErrNoRows) {
		return pgx.ErrNoRows
	}
	if err != nil {
		log.Printf("SQLite row failed: %v; SQL: %s", err, r.query)
	}
	if err == nil {
		err = decode()
	}
	return err
}

func sqliteScanDest(dest []any) ([]any, func() error) {
	scanDest := append([]any(nil), dest...)
	type pendingJSON struct {
		index int
		raw   sql.NullString
	}
	var pending []*pendingJSON
	for i, target := range dest {
		t := reflect.TypeOf(target)
		if t == nil || t.Kind() != reflect.Ptr {
			continue
		}
		if t.Elem().Kind() == reflect.Slice && t.Elem().Elem().Kind() != reflect.Uint8 {
			p := &pendingJSON{index: i}
			scanDest[i] = &p.raw
			pending = append(pending, p)
		}
	}
	return scanDest, func() error {
		for _, p := range pending {
			if !p.raw.Valid {
				continue
			}
			raw := p.raw.String
			if raw == "{}" {
				raw = "[]"
			}
			if err := json.Unmarshal([]byte(raw), dest[p.index]); err != nil {
				return err
			}
		}
		return nil
	}
}

type sqliteRows struct{ rows *sql.Rows }

func (r *sqliteRows) Close()                        { _ = r.rows.Close() }
func (r *sqliteRows) Err() error                    { return r.rows.Err() }
func (r *sqliteRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("SELECT") }
func (r *sqliteRows) FieldDescriptions() []pgconn.FieldDescription {
	cols, _ := r.rows.Columns()
	out := make([]pgconn.FieldDescription, len(cols))
	for i, name := range cols {
		out[i].Name = name
	}
	return out
}
func (r *sqliteRows) Next() bool { return r.rows.Next() }
func (r *sqliteRows) Scan(dest ...any) error {
	scanDest, decode := sqliteScanDest(dest)
	if err := r.rows.Scan(scanDest...); err != nil {
		log.Printf("SQLite scan failed: %v", err)
		return err
	}
	if err := decode(); err != nil {
		log.Printf("SQLite decode failed: %v", err)
		return err
	}
	return nil
}
func (r *sqliteRows) Values() ([]any, error) {
	cols, err := r.rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := r.rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	return values, nil
}
func (r *sqliteRows) RawValues() [][]byte  { return nil }
func (r *sqliteRows) Conn() *pgx.Conn      { return nil }
func (r *sqliteRows) TypeMap() *pgtype.Map { return nil }

type sqliteTx struct{ tx *sql.Tx }

var (
	episodeSE = regexp.MustCompile(`(?i)s(\d{1,2})[ ._-]?e(\d{1,3})`)
	episodeX  = regexp.MustCompile(`(?i)(?:^|[ ._-])(\d{1,2})x(\d{2,3})(?:[ ._-]|$)`)
)

func reassignSQLiteLibraries(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE media_file SET library_id = (
		SELECT lr.library_id FROM library_root lr
		WHERE lr.storage_location_id = media_file.storage_location_id
		  AND (lr.path = '' OR media_file.relative_path = lr.path
		       OR substr(media_file.relative_path, 1, length(lr.path) + 1) = lr.path || '/')
		ORDER BY length(lr.path) DESC, lr.id LIMIT 1)`); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE media_item SET library_id = (
		SELECT mf.library_id FROM media_file mf WHERE mf.media_item_id = media_item.id
		ORDER BY mf.available DESC, mf.library_id IS NULL, mf.id LIMIT 1)`)
	return err
}

func classifySQLiteTitles(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT mi.id, COALESCE((SELECT file_name FROM media_file
		WHERE media_item_id = mi.id ORDER BY id LIMIT 1), '')
		FROM media_item mi JOIN library l ON l.id = mi.library_id
		WHERE l.mode = 'entertainment' AND mi.kind IS NULL`)
	if err != nil {
		return err
	}
	type title struct {
		id       int64
		filename string
	}
	var titles []title
	for rows.Next() {
		var item title
		if err := rows.Scan(&item.id, &item.filename); err != nil {
			rows.Close()
			return err
		}
		titles = append(titles, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range titles {
		match := episodeSE.FindStringSubmatch(item.filename)
		if match == nil {
			match = episodeX.FindStringSubmatch(item.filename)
		}
		if match == nil {
			if _, err := tx.ExecContext(ctx, `UPDATE media_item SET kind = 'movie' WHERE id = ?`, item.id); err != nil {
				return err
			}
			continue
		}
		season, _ := strconv.Atoi(match[1])
		episode, _ := strconv.Atoi(match[2])
		if _, err := tx.ExecContext(ctx, `UPDATE media_item SET kind = 'episode', season_number = COALESCE(season_number, ?), episode_number = COALESCE(episode_number, ?) WHERE id = ?`, season, episode, item.id); err != nil {
			return err
		}
	}
	return nil
}

func (t *sqliteTx) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("nested SQLite transactions are not supported")
}
func (t *sqliteTx) Commit(ctx context.Context) error   { return t.tx.Commit() }
func (t *sqliteTx) Rollback(ctx context.Context) error { return t.tx.Rollback() }
func (t *sqliteTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("SQLite CopyFrom is unavailable")
}
func (t *sqliteTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (t *sqliteTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (t *sqliteTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("SQLite Prepare is unavailable")
}
func (t *sqliteTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "SELECT reassign_libraries()" {
		return pgconn.NewCommandTag("SELECT 1"), reassignSQLiteLibraries(ctx, t.tx)
	}
	if trimmed == "SELECT classify_titles()" {
		return pgconn.NewCommandTag("SELECT 1"), classifySQLiteTitles(ctx, t.tx)
	}
	result, err := t.tx.ExecContext(ctx, sqliteQuery(query), sqliteArgs(args)...)
	if err != nil {
		log.Printf("SQLite transaction exec failed: %v; SQL: %s", err, query)
		return pgconn.CommandTag{}, err
	}
	return sqliteTag(query, result), nil
}
func (t *sqliteTx) Query(ctx context.Context, query string, args ...any) (pgx.Rows, error) {
	rows, err := t.tx.QueryContext(ctx, sqliteQuery(query), sqliteArgs(args)...)
	if err != nil {
		log.Printf("SQLite transaction query failed: %v; SQL: %s", err, query)
		return nil, err
	}
	return &sqliteRows{rows: rows}, nil
}
func (t *sqliteTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return sqliteRow{row: t.tx.QueryRowContext(ctx, sqliteQuery(query), sqliteArgs(args)...), query: query}
}
func (t *sqliteTx) Conn() *pgx.Conn { return nil }
