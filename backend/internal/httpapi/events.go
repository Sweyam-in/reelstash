package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	// sseHeartbeat keeps proxies from closing an idle stream.
	sseHeartbeat = 20 * time.Second
	// jobsFeedBusy is how often job state is re-read while something runs
	// (scan progress lives in tables the runner does not signal on).
	jobsFeedBusy = time.Second
	jobsFeedIdle = 15 * time.Second
	// jobsFeedCoalesce lets a burst of job changes produce one snapshot.
	jobsFeedCoalesce = 250 * time.Millisecond
	jobsFeedLimit    = 200
)

// runJobsFeed publishes the admin Jobs page snapshot to connected admins
// whenever it changes. It wakes on job-state signals and, while jobs are
// active, on a short timer; with nobody listening it does no database work.
func (a *API) runJobsFeed(ctx context.Context) {
	var last []byte
	wait := jobsFeedIdle
	for {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-a.Hub.Kicked():
		case <-timer.C:
		}
		timer.Stop()
		time.Sleep(jobsFeedCoalesce)

		wait = jobsFeedIdle
		if a.Hub.AdminSubscribers() == 0 {
			last = nil // the next listener gets a fresh snapshot on connect
			continue
		}
		snapshot, err := a.jobsSnapshot(ctx, "", "", jobsFeedLimit)
		if err != nil {
			continue
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			continue
		}
		if !bytes.Equal(data, last) {
			last = data
			a.Hub.Publish("jobs", json.RawMessage(data), true)
		}
		if jobsActive(snapshot) {
			wait = jobsFeedBusy
		}
	}
}

func jobsActive(snapshot map[string]any) bool {
	counts, _ := snapshot["counts"].(map[string]int64)
	if counts["RUNNING"]+counts["PENDING"] > 0 {
		return true
	}
	thumbs, _ := snapshot["thumbnailsPending"].(int64)
	previews, _ := snapshot["previewsPending"].(int64)
	return thumbs+previews > 0
}

// handleEvents is the server-sent-events stream. Admins receive live job
// snapshots; everyone receives notifications.
func (a *API) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is not supported")
		return
	}
	user, _ := UserFromContext(r.Context())
	isAdmin := user.Role == "ADMIN"

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // tell nginx not to buffer the stream

	sub, unsubscribe := a.Hub.Subscribe(isAdmin)
	defer unsubscribe()

	fmt.Fprint(w, "retry: 3000\n\n")
	if isAdmin {
		if snapshot, err := a.jobsSnapshot(r.Context(), "", "", jobsFeedLimit); err == nil {
			if data, err := json.Marshal(snapshot); err == nil {
				fmt.Fprintf(w, "event: jobs\ndata: %s\n\n", data)
			}
		}
	}
	fmt.Fprint(w, "event: ready\ndata: {}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(sseHeartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev := <-sub.C:
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Name, ev.Data)
			flusher.Flush()
		}
	}
}
