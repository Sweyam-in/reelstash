package schedule

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"strings"

	"reelstash/backend/internal/diskinfo"
	"reelstash/backend/internal/scanner"
)

// pollShares scans libraries whose folders live on a network share, or whose
// inotify watch could not start. The first pass only remembers the tree; a
// later pass queues a library scan when a video or subtitle file changed.
func (w *Watcher) pollShares(ctx context.Context) {
	targets, err := w.watchTargets(ctx)
	if err != nil {
		log.Printf("watch: poll: %v", err)
		return
	}
	for id, target := range targets {
		if !w.shouldPoll(id, target.roots) {
			continue
		}
		stamp := treeStamp(target)
		w.mu.Lock()
		prev, seen := w.stamps[id]
		w.stamps[id] = stamp
		w.mu.Unlock()
		if !seen || prev == stamp {
			continue
		}
		if err := w.enqueue(ctx, id); err != nil {
			log.Printf("watch: poll library %d: %v", id, err)
		}
	}
}

func (w *Watcher) shouldPoll(id int64, roots []string) bool {
	w.mu.Lock()
	_, watching := w.active[id]
	w.mu.Unlock()
	if !watching {
		return true
	}
	for _, root := range roots {
		mount, err := diskinfo.MountOf(root, diskinfo.DefaultTimeout)
		if err != nil || diskinfo.Network(mount.FSType) {
			return true
		}
	}
	return false
}

func treeStamp(target watchTarget) string {
	h := sha256.New()
	for _, root := range target.roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != root && (scanner.SkipDir(d.Name()) || target.skip[path]) {
					return fs.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if !scanner.IsVideoExt(ext) && !isSubtitleName(ext) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			fmt.Fprintf(h, "%s %d %d\n", path, info.Size(), info.ModTime().UnixNano())
			return nil
		})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func isSubtitleName(ext string) bool {
	switch ext {
	case ".srt", ".vtt", ".ass", ".ssa":
		return true
	default:
		return false
	}
}
