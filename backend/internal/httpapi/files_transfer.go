package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/jobs"
)

// FileTransferJobKind is the background job behind the file manager's paste:
// a move or copy of several entries, possibly onto another disk, where the
// bytes have to be copied and can take a long time.
const FileTransferJobKind = "FILE_TRANSFER"

// transferProgressEvery throttles progress writes (each one is a DB update
// and a jobs-feed wake-up).
const transferProgressEvery = 500 * time.Millisecond

type fileTransferPayload struct {
	Mode                  string   `json:"mode"`              // "move" or "copy"
	StorageLocationID     int64    `json:"storageLocationId"` // source
	DestStorageLocationID int64    `json:"destStorageLocationId"`
	Dir                   string   `json:"dir"`   // source folder, relative
	Items                 []string `json:"items"` // entry names inside Dir
	DestPath              string   `json:"destPath"`
	// Done maps each finished item to the name it landed under, so a job
	// resumed after a restart (or retried) skips what it already did.
	Done map[string]string `json:"done,omitempty"`
}

func (p fileTransferPayload) verb() string {
	if p.Mode == "move" {
		return "moved"
	}
	return "copied"
}

// startFileTransfer validates a paste and queues it as a FILE_TRANSFER job.
// Problems that can be seen up front (missing items, a bad destination) are
// refused here so the user hears about them straight away.
func (a *API) startFileTransfer(w http.ResponseWriter, r *http.Request) {
	var p fileTransferPayload
	if err := decodeJSON(r, &p); err != nil || len(p.Items) == 0 {
		writeError(w, http.StatusBadRequest, "items are required")
		return
	}
	if p.Mode != "move" && p.Mode != "copy" {
		writeError(w, http.StatusBadRequest, `mode must be "move" or "copy"`)
		return
	}
	if p.DestStorageLocationID == 0 {
		p.DestStorageLocationID = p.StorageLocationID
	}
	p.Done = nil
	for _, name := range p.Items {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
			writeError(w, http.StatusBadRequest, "invalid item name "+name)
			return
		}
		t, status, msg := checkTransfer(r.Context(), a.DB, p.StorageLocationID, filepath.Join(p.Dir, name), p.DestStorageLocationID, p.DestPath, p.verb())
		if msg == "" && p.Mode == "move" && filepath.Join(t.absDestDir, name) == t.absSrc {
			status, msg = http.StatusBadRequest, "the item is already in that folder"
		}
		if msg != "" {
			writeError(w, status, name+": "+msg)
			return
		}
	}
	if a.Jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "background jobs are not available")
		return
	}
	id, err := a.Jobs.Enqueue(r.Context(), FileTransferJobKind, p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue the transfer")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"backgroundJobId": id})
}

// FileTransferJob runs a FILE_TRANSFER job (a jobs.Handler). Items are
// handled one at a time; one that fails is reported and the rest carry on.
// Progress is in KiB (progress_done/total are 32-bit), labelled with the
// item being worked on.
func FileTransferJob(ctx context.Context, db *pgxpool.Pool, jobID int64, raw json.RawMessage) (any, error) {
	var p fileTransferPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Mode != "move" && p.Mode != "copy" {
		return nil, errors.New(`mode must be "move" or "copy"`)
	}
	if p.DestStorageLocationID == 0 {
		p.DestStorageLocationID = p.StorageLocationID
	}
	if p.Done == nil {
		p.Done = map[string]string{}
	}

	type pending struct {
		name string
		t    fileTransfer
		size int64
	}
	var todo []pending
	var failures []string
	var total int64
	for _, name := range p.Items {
		if _, done := p.Done[name]; done {
			continue
		}
		t, _, msg := checkTransfer(ctx, db, p.StorageLocationID, filepath.Join(p.Dir, name), p.DestStorageLocationID, p.DestPath, p.verb())
		if msg != "" {
			failures = append(failures, name+": "+msg)
			continue
		}
		size := treeSize(t.absSrc)
		total += size
		todo = append(todo, pending{name, t, size})
	}

	prog := &transferProgress{db: db, jobID: jobID, total: total}
	for _, it := range todo {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		start := prog.current()
		prog.setLabel(it.name)
		landed, err := transferOne(ctx, db, p, it.name, it.t, prog.add)
		prog.set(start + it.size)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			failures = append(failures, it.name+": "+err.Error())
			continue
		}
		p.Done[it.name] = landed
		saveTransferDone(db, jobID, p.Done)
	}
	prog.flush()

	landed := []string{}
	for _, name := range p.Items {
		if to, ok := p.Done[name]; ok {
			landed = append(landed, to)
		}
	}
	if len(landed) == 0 && len(failures) > 0 {
		return nil, errors.New(strings.Join(failures, "; "))
	}
	return map[string]any{
		"mode": p.Mode, "transferred": len(landed), "failed": len(failures), "failures": failures,
		"landed": landed, "bytes": total, "destStorageLocationId": p.DestStorageLocationID, "destPath": p.DestPath,
	}, nil
}

// transferOne moves or copies one entry and returns the name it landed under.
func transferOne(ctx context.Context, db *pgxpool.Pool, p fileTransferPayload, name string, t fileTransfer, onBytes func(int64)) (string, error) {
	if p.Mode == "copy" {
		landed := uniqueCopyName(t.absDestDir, name)
		dst := filepath.Join(t.absDestDir, landed)
		if err := copyTree(ctx, t.absSrc, dst, false, onBytes); err != nil {
			os.RemoveAll(dst)
			return "", err
		}
		return landed, nil
	}
	dst := filepath.Join(t.absDestDir, name)
	if _, err := os.Lstat(dst); err == nil {
		return "", errors.New("a file already exists with that name in the destination")
	}
	if err := moveAcrossDevices(ctx, t.absSrc, dst, onBytes); err != nil {
		return "", err
	}
	// The move happened; keep the rows in step even if the job is being cancelled.
	followMovedRows(context.Background(), db, t, filepath.ToSlash(filepath.Join(p.Dir, name)), filepath.ToSlash(filepath.Join(p.DestPath, name)))
	return name, nil
}

// saveTransferDone records finished items on the job row. Best-effort: losing
// it only means a resumed copy duplicates an item, or a resumed move reports
// an item it already moved as missing.
func saveTransferDone(db *pgxpool.Pool, jobID int64, done map[string]string) {
	if jobID == 0 {
		return
	}
	data, err := json.Marshal(done)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	db.Exec(ctx, `UPDATE background_job SET payload = jsonb_set(payload, '{done}', $2::jsonb) WHERE id = $1`, jobID, data)
}

// treeSize totals the regular files under path (symlinks aren't followed,
// matching copyTree).
func treeSize(path string) int64 {
	var total int64
	filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// transferProgress reports bytes copied to the job row, at most every
// transferProgressEvery.
type transferProgress struct {
	db    *pgxpool.Pool
	jobID int64
	total int64

	mu       sync.Mutex
	done     int64
	label    string
	reported time.Time
}

func (t *transferProgress) current() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.done
}

func (t *transferProgress) add(n int64) {
	t.mu.Lock()
	t.done += n
	t.mu.Unlock()
	t.report(false)
}

func (t *transferProgress) set(n int64) {
	t.mu.Lock()
	t.done = n
	t.mu.Unlock()
	t.report(false)
}

func (t *transferProgress) setLabel(label string) {
	t.mu.Lock()
	t.label = label
	t.mu.Unlock()
	t.report(true)
}

func (t *transferProgress) flush() { t.report(true) }

func (t *transferProgress) report(force bool) {
	t.mu.Lock()
	if !force && time.Since(t.reported) < transferProgressEvery {
		t.mu.Unlock()
		return
	}
	t.reported = time.Now()
	done, total, label := t.done/1024, t.total/1024, t.label
	t.mu.Unlock()
	if total < 1 {
		done, total = 0, 1 // empty files: still show a determinate bar
	}
	jobs.ReportProgress(t.db, t.jobID, int(min(done, total)), int(total), label)
}
