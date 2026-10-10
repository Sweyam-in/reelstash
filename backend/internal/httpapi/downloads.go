package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"reelstash/backend/internal/downloads"
	"reelstash/backend/internal/pathguard"
	"reelstash/backend/internal/qbit"
)

// WithDownloads gives the API the qBittorrent link behind the Downloads workspace.
func WithDownloads(s *downloads.Service) Option { return func(a *API) { a.Downloads = s } }

// downloadsRoutes mounts /api/downloads (admins only).
func (a *API) downloadsRoutes(r chi.Router) {
	r.Use(a.requireAdmin)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a.Downloads == nil {
				writeError(w, http.StatusServiceUnavailable, "downloads are not available")
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	r.Get("/", a.downloadsSnapshot)
	r.Post("/add", a.addTorrents)
	r.Route("/torrents", func(r chi.Router) {
		r.Post("/action", a.torrentAction)
		r.Post("/delete", a.deleteTorrents)
		r.Post("/category", a.setTorrentCategory)
		r.Post("/tags", a.setTorrentTags)
		r.Post("/move", a.moveTorrents)
		r.Get("/{hash}", a.torrentDetails)
		r.Get("/{hash}/peers", a.torrentPeers)
		r.Post("/{hash}/rename", a.renameTorrent)
		r.Post("/{hash}/files", a.setTorrentFilePriority)
		r.Post("/{hash}/limits", a.setTorrentLimits)
		r.Post("/{hash}/apply-rules", a.applyTorrentRules)
	})
	r.Post("/categories", a.createTorrentCategory)
	r.Put("/categories/{name}", a.editTorrentCategory)
	r.Delete("/categories/{name}", a.deleteTorrentCategory)
	r.Post("/tags", a.createTorrentTag)
	r.Delete("/tags/{name}", a.deleteTorrentTag)
	r.Post("/alt-speed", a.toggleAltSpeed)
	r.Get("/preferences", a.getTorrentPreferences)
	r.Put("/preferences", a.updateTorrentPreferences)
	r.Get("/settings", a.getDownloadSettings)
	r.Put("/settings", a.updateDownloadSettings)
	r.Post("/settings/test", a.testDownloadSettings)
	r.Put("/folders", a.updateDownloadFolders)
	r.Post("/folders/fix", a.fixDownloadFolders)
	r.Route("/rules", func(r chi.Router) {
		r.Get("/", a.listDownloadRules)
		r.Post("/", a.createDownloadRule)
		r.Put("/order", a.reorderDownloadRules)
		r.Get("/test", a.testDownloadRules)
		r.Put("/{id}", a.updateDownloadRule)
		r.Delete("/{id}", a.deleteDownloadRule)
	})
	r.Get("/history", a.downloadHistory)
	r.Delete("/history/{hash}", a.forgetDownload)
}

// downloadsError maps qBittorrent problems to a status: 503 when it is not
// set up or can't be reached, 502 when it answered with an error.
func downloadsError(w http.ResponseWriter, err error) {
	if errors.Is(err, qbit.ErrNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, err.Error())
}

// qbitCall runs fn with the current client and a bounded context.
func (a *API) qbitCall(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, c *qbit.Client) error) bool {
	client, _, err := a.Downloads.Client(r.Context())
	if err != nil {
		downloadsError(w, err)
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := fn(ctx, client); err != nil {
		downloadsError(w, err)
		return false
	}
	a.Downloads.Kick()
	return true
}

func (a *API) downloadsSnapshot(w http.ResponseWriter, r *http.Request) {
	snap, err := a.Downloads.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load downloads")
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

type hashesRequest struct {
	Hashes []string `json:"hashes"`
}

func (h hashesRequest) valid() bool {
	if len(h.Hashes) == 0 {
		return false
	}
	for _, x := range h.Hashes {
		if x == "" || strings.ContainsAny(x, "|\n") {
			return false
		}
	}
	return true
}

// torrentActions are the hashes-only actions the workspace offers.
var torrentActions = map[string]string{
	"recheck": "recheck", "reannounce": "reannounce",
	"topPrio": "topPrio", "bottomPrio": "bottomPrio", "increasePrio": "increasePrio", "decreasePrio": "decreasePrio",
	"toggleSequential": "toggleSequentialDownload", "toggleFirstLast": "toggleFirstLastPiecePrio",
}

func (a *API) torrentAction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		hashesRequest
		Action string `json:"action"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.valid() {
		writeError(w, http.StatusBadRequest, "hashes and an action are required")
		return
	}
	var call func(ctx context.Context, c *qbit.Client) error
	switch req.Action {
	case "stop":
		call = func(ctx context.Context, c *qbit.Client) error { return c.Stop(ctx, req.Hashes) }
	case "start":
		call = func(ctx context.Context, c *qbit.Client) error { return c.Start(ctx, req.Hashes) }
	case "forceStart", "noForceStart":
		call = func(ctx context.Context, c *qbit.Client) error {
			return c.SetForceStart(ctx, req.Hashes, req.Action == "forceStart")
		}
	default:
		name, ok := torrentActions[req.Action]
		if !ok {
			writeError(w, http.StatusBadRequest, "unknown action "+req.Action)
			return
		}
		call = func(ctx context.Context, c *qbit.Client) error { return c.Action(ctx, name, req.Hashes) }
	}
	if a.qbitCall(w, r, call) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) deleteTorrents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		hashesRequest
		DeleteFiles bool `json:"deleteFiles"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.valid() {
		writeError(w, http.StatusBadRequest, "hashes are required")
		return
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error { return c.Delete(ctx, req.Hashes, req.DeleteFiles) }) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) setTorrentCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		hashesRequest
		Category string `json:"category"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.valid() {
		writeError(w, http.StatusBadRequest, "hashes are required")
		return
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error { return c.SetCategory(ctx, req.Hashes, req.Category) }) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) setTorrentTags(w http.ResponseWriter, r *http.Request) {
	var req struct {
		hashesRequest
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.valid() {
		writeError(w, http.StatusBadRequest, "hashes are required")
		return
	}
	ok := a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		if len(req.Add) > 0 {
			if err := c.AddTags(ctx, req.Hashes, strings.Join(req.Add, ",")); err != nil {
				return err
			}
		}
		if len(req.Remove) > 0 {
			return c.RemoveTags(ctx, req.Hashes, strings.Join(req.Remove, ","))
		}
		return nil
	})
	if ok {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// folderRequest names a folder on a storage location.
type folderRequest struct {
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"`
}

func (a *API) moveTorrents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		hashesRequest
		folderRequest
		Scan *bool `json:"scan"`
	}
	if err := decodeJSON(r, &req); err != nil || !req.valid() || req.StorageLocationID == 0 {
		writeError(w, http.StatusBadRequest, "hashes and a destination folder are required")
		return
	}
	dest, err := downloads.ResolveDir(r.Context(), a.DB, req.StorageLocationID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	scan := req.Scan == nil || *req.Scan
	if err := a.Downloads.MoveTo(r.Context(), req.Hashes, dest, scan); err != nil {
		downloadsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "dest": dest})
}

func (a *API) torrentDetails(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	out := map[string]any{}
	ok := a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		props, err := c.Properties(ctx, hash)
		if err != nil {
			return err
		}
		files, err := c.Files(ctx, hash)
		if err != nil {
			return err
		}
		trackers, err := c.Trackers(ctx, hash)
		if err != nil {
			return err
		}
		out["properties"], out["files"], out["trackers"] = props, files, trackers
		return nil
	})
	if ok {
		writeJSON(w, http.StatusOK, out)
	}
}

func (a *API) torrentPeers(w http.ResponseWriter, r *http.Request) {
	var peers map[string]any
	ok := a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		var err error
		peers, err = c.Peers(ctx, chi.URLParam(r, "hash"))
		return err
	})
	if ok {
		list := make([]map[string]any, 0, len(peers))
		for addr, p := range peers {
			if m, ok := p.(map[string]any); ok {
				m["address"] = addr
				list = append(list, m)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"peers": list})
	}
}

func (a *API) renameTorrent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "a name is required")
		return
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		return c.Rename(ctx, chi.URLParam(r, "hash"), strings.TrimSpace(req.Name))
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) setTorrentFilePriority(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs      []int `json:"ids"`
		Priority int   `json:"priority"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "file ids are required")
		return
	}
	switch req.Priority {
	case 0, 1, 6, 7:
	default:
		writeError(w, http.StatusBadRequest, "priority must be 0 (skip), 1 (normal), 6 (high) or 7 (maximum)")
		return
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		return c.FilePriority(ctx, chi.URLParam(r, "hash"), req.IDs, req.Priority)
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) setTorrentLimits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Download *int64 `json:"download"`
		Upload   *int64 `json:"upload"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	dl, ul := int64(-1), int64(-1)
	if req.Download != nil && *req.Download >= 0 {
		dl = *req.Download
	}
	if req.Upload != nil && *req.Upload >= 0 {
		ul = *req.Upload
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		return c.SetLimits(ctx, []string{chi.URLParam(r, "hash")}, dl, ul)
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) applyTorrentRules(w http.ResponseWriter, r *http.Request) {
	info, err := a.Downloads.ApplyRules(r.Context(), chi.URLParam(r, "hash"))
	if err != nil && info.Status == "" {
		downloadsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

const maxTorrentFileBytes = 10 << 20

func (a *API) addTorrents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request (it may be too large)")
		return
	}
	var urls []string
	for _, line := range strings.Split(r.FormValue("urls"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			urls = append(urls, line)
		}
	}
	var files []qbit.TorrentFile
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["torrents"] {
			if fh.Size > maxTorrentFileBytes {
				writeError(w, http.StatusBadRequest, fh.Filename+" is too large to be a .torrent file")
				return
			}
			f, err := fh.Open()
			if err != nil {
				writeError(w, http.StatusBadRequest, "could not read "+fh.Filename)
				return
			}
			data, err := io.ReadAll(io.LimitReader(f, maxTorrentFileBytes))
			f.Close()
			if err != nil {
				writeError(w, http.StatusBadRequest, "could not read "+fh.Filename)
				return
			}
			files = append(files, qbit.TorrentFile{Name: fh.Filename, Data: data})
		}
	}
	if len(urls) == 0 && len(files) == 0 {
		writeError(w, http.StatusBadRequest, "add a magnet link, a URL or a .torrent file")
		return
	}
	opts := qbit.AddOptions{
		Category:           r.FormValue("category"),
		Tags:               r.FormValue("tags"),
		Stopped:            r.FormValue("stopped") == "true",
		SkipChecking:       r.FormValue("skipChecking") == "true",
		Sequential:         r.FormValue("sequential") == "true",
		FirstLastPiecePrio: r.FormValue("firstLast") == "true",
		Rename:             strings.TrimSpace(r.FormValue("rename")),
	}
	if loc, _ := strconv.ParseInt(r.FormValue("storageLocationId"), 10, 64); loc != 0 {
		dest, err := downloads.ResolveDir(r.Context(), a.DB, loc, r.FormValue("path"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if pathguard.Load(r.Context(), a.DB).Locked(dest) {
			writeError(w, http.StatusBadRequest, "pick a save folder outside the in-progress folder; qBittorrent uses that one by itself while downloading")
			return
		}
		opts.SavePath = a.Downloads.Paths().Remote(dest)
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error { return c.Add(ctx, urls, files, opts) }) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": len(urls) + len(files)})
	}
}

// categoryRequest is a category with an optional save folder.
type categoryRequest struct {
	Name              string `json:"name"`
	StorageLocationID int64  `json:"storageLocationId"`
	Path              string `json:"path"`
}

func (a *API) categorySavePath(w http.ResponseWriter, r *http.Request, req categoryRequest) (string, bool) {
	if req.StorageLocationID == 0 {
		return "", true
	}
	dest, err := downloads.ResolveDir(r.Context(), a.DB, req.StorageLocationID, req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return a.Downloads.Paths().Remote(dest), true
}

func (a *API) createTorrentCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "a category name is required")
		return
	}
	savePath, ok := a.categorySavePath(w, r, req)
	if ok && a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		return c.CreateCategory(ctx, strings.TrimSpace(req.Name), savePath)
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) editTorrentCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	savePath, ok := a.categorySavePath(w, r, req)
	if ok && a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		return c.EditCategory(ctx, chi.URLParam(r, "name"), savePath)
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) deleteTorrentCategory(w http.ResponseWriter, r *http.Request) {
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error {
		return c.RemoveCategories(ctx, []string{chi.URLParam(r, "name")})
	}) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) createTorrentTag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" || strings.Contains(req.Name, ",") {
		writeError(w, http.StatusBadRequest, "a tag name without commas is required")
		return
	}
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error { return c.CreateTags(ctx, strings.TrimSpace(req.Name)) }) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) deleteTorrentTag(w http.ResponseWriter, r *http.Request) {
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error { return c.DeleteTags(ctx, chi.URLParam(r, "name")) }) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (a *API) toggleAltSpeed(w http.ResponseWriter, r *http.Request) {
	if a.qbitCall(w, r, func(ctx context.Context, c *qbit.Client) error { return c.ToggleAltSpeed(ctx) }) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// torrentPrefKinds are the qBittorrent settings the workspace may read and
// change, with their JSON kind. Everything else (including its "run a
// program" options and WebUI credentials) stays out of reach.
var torrentPrefKinds = map[string]string{
	"dl_limit": "int", "up_limit": "int", "alt_dl_limit": "int", "alt_up_limit": "int",
	"queueing_enabled": "bool", "max_active_downloads": "int", "max_active_uploads": "int", "max_active_torrents": "int",
	"max_ratio_enabled": "bool", "max_ratio": "float", "max_seeding_time_enabled": "bool", "max_seeding_time": "int",
	"max_ratio_act": "int", "listen_port": "int", "upnp": "bool", "dht": "bool", "pex": "bool", "lsd": "bool",
	"incomplete_files_ext": "bool", "preallocate_all": "bool", "max_connec": "int", "max_connec_per_torrent": "int",
	"max_uploads": "int", "max_uploads_per_torrent": "int",
	"max_active_checking_torrents": "int", "dont_count_slow_torrents": "bool", "slow_torrent_dl_rate_threshold": "int",
	"slow_torrent_ul_rate_threshold": "int", "slow_torrent_inactive_timer": "int", "add_to_top_of_queue": "bool",
}

func (a *API) getTorrentPreferences(w http.ResponseWriter, r *http.Request) {
	prefs, err := a.Downloads.Prefs(r.Context())
	if err != nil {
		downloadsError(w, err)
		return
	}
	out := map[string]any{}
	for k := range torrentPrefKinds {
		if v, ok := prefs[k]; ok {
			out[k] = v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"values": out})
}

func (a *API) updateTorrentPreferences(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := decodeJSON(r, &req); err != nil || len(req) == 0 {
		writeError(w, http.StatusBadRequest, "settings are required")
		return
	}
	clean := map[string]any{}
	for k, v := range req {
		kind, ok := torrentPrefKinds[k]
		if !ok {
			writeError(w, http.StatusBadRequest, "the setting "+k+" can't be changed from ReelStash")
			return
		}
		switch kind {
		case "bool":
			b, ok := v.(bool)
			if !ok {
				writeError(w, http.StatusBadRequest, k+" must be true or false")
				return
			}
			clean[k] = b
		case "int", "float":
			n, ok := v.(float64)
			if !ok {
				writeError(w, http.StatusBadRequest, k+" must be a number")
				return
			}
			if kind == "int" {
				clean[k] = int64(n)
			} else {
				clean[k] = n
			}
		}
	}
	if act, ok := clean["max_ratio_act"].(int64); ok && act != 0 && act != 1 {
		writeError(w, http.StatusBadRequest, "max_ratio_act must be 0 (stop) or 1 (remove)")
		return
	}
	if err := a.Downloads.SetPrefs(r.Context(), clean); err != nil {
		downloadsError(w, err)
		return
	}
	a.getTorrentPreferences(w, r)
}

// downloadSettingsView is the settings form, password masked.
func (a *API) downloadSettingsView(ctx context.Context) map[string]any {
	s := downloads.LoadSettings(ctx, a.DB)
	env := a.Downloads.Env
	conn := s.Connection(env)
	out := map[string]any{
		"url": s.URL, "username": s.Username, "passwordSet": s.Password != "",
		"envUrl": env.URL, "envUsername": env.Username, "envPasswordSet": env.Password != "",
		"effectiveUrl": conn.URL,
		"pathFrom":     s.PathFrom, "pathTo": s.PathTo,
		"autoMove": s.AutoMove, "moveTag": s.Tag(),
		"status": a.Downloads.Status(ctx),
	}
	if s.IncompleteDir != "" {
		if loc, err := downloads.Locate(ctx, a.DB, s.IncompleteDir); err == nil {
			out["incomplete"] = loc
		} else {
			out["incomplete"] = map[string]any{"path": s.IncompleteDir}
		}
	}
	if prefs, err := a.Downloads.Prefs(ctx); err == nil {
		paths := s.Paths()
		if save := paths.Local(qbit.Str(prefs, "save_path")); save != "" {
			if loc, err := downloads.Locate(ctx, a.DB, save); err == nil {
				out["savePath"] = loc
			} else {
				out["savePath"] = map[string]any{"path": save, "outside": true}
			}
		}
	}
	return out
}

func (a *API) getDownloadSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.downloadSettingsView(r.Context()))
}

type downloadSettingsRequest struct {
	URL      *string `json:"url"`
	Username *string `json:"username"`
	Password *string `json:"password"`
	PathFrom *string `json:"pathFrom"`
	PathTo   *string `json:"pathTo"`
	AutoMove *bool   `json:"autoMove"`
	MoveTag  *string `json:"moveTag"`
}

// merge applies the fields present: an absent field is left alone, an empty
// string clears it.
func (req downloadSettingsRequest) merge(s downloads.Settings) downloads.Settings {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&s.URL, req.URL)
	set(&s.Username, req.Username)
	if req.Password != nil {
		s.Password = *req.Password
	}
	set(&s.PathFrom, req.PathFrom)
	set(&s.PathTo, req.PathTo)
	set(&s.MoveTag, req.MoveTag)
	if req.AutoMove != nil {
		if *req.AutoMove && !s.AutoMove {
			s.RulesSince = time.Now().Unix()
		}
		s.AutoMove = *req.AutoMove
	}
	return s
}

func (a *API) updateDownloadSettings(w http.ResponseWriter, r *http.Request) {
	var req downloadSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	s := req.merge(downloads.LoadSettings(r.Context(), a.DB))
	if s.URL != "" && !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
		writeError(w, http.StatusBadRequest, "the address must start with http:// or https://")
		return
	}
	if (s.PathFrom == "") != (s.PathTo == "") {
		writeError(w, http.StatusBadRequest, "set both sides of the path translation, or neither")
		return
	}
	if err := downloads.SaveSettings(r.Context(), a.DB, s); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	a.Downloads.Kick()
	writeJSON(w, http.StatusOK, a.downloadSettingsView(r.Context()))
}

// testDownloadSettings signs in with the submitted connection (saved values
// fill the gaps) without saving it.
func (a *API) testDownloadSettings(w http.ResponseWriter, r *http.Request) {
	var req downloadSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cfg := req.merge(downloads.LoadSettings(r.Context(), a.DB)).Connection(a.Downloads.Env)
	if !cfg.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": qbit.ErrNotConfigured.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	client := qbit.New(cfg)
	version, err := client.Version(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	api, _ := client.APIVersion(ctx)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version, "apiVersion": api})
}

// updateDownloadFolders sets the in-progress folder (locked, and handed to
// qBittorrent as its incomplete folder) and qBittorrent's default save folder.
func (a *API) updateDownloadFolders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Incomplete      *folderRequest `json:"incomplete"`
		ClearIncomplete bool           `json:"clearIncomplete"`
		SavePath        *folderRequest `json:"savePath"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	s := downloads.LoadSettings(ctx, a.DB)
	incomplete := s.IncompleteDir
	if req.ClearIncomplete {
		incomplete = ""
	} else if req.Incomplete != nil {
		dir, err := downloads.ResolveDir(ctx, a.DB, req.Incomplete.StorageLocationID, req.Incomplete.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := downloads.CheckIncompleteDir(ctx, a.DB, dir); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		incomplete = dir
	}
	var savePath string
	if req.SavePath != nil {
		dir, err := downloads.ResolveDir(ctx, a.DB, req.SavePath.StorageLocationID, req.SavePath.Path)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if incomplete != "" && (dir == incomplete || strings.HasPrefix(dir, incomplete+"/")) {
			writeError(w, http.StatusBadRequest, "the save folder can't be inside the in-progress folder")
			return
		}
		savePath = dir
	}
	if incomplete != s.IncompleteDir {
		if err := downloads.SetProtected(ctx, a.DB, incomplete); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save the in-progress folder")
			return
		}
		s.IncompleteDir = incomplete
		if err := downloads.SaveSettings(ctx, a.DB, s); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save settings")
			return
		}
	}
	// Tell qBittorrent. The saved folder stands even if it can't be reached:
	// the drift warning offers to apply it once it can.
	prefs := map[string]any{}
	if req.ClearIncomplete {
		prefs["temp_path_enabled"] = false
	} else if req.Incomplete != nil {
		prefs["temp_path_enabled"], prefs["temp_path"] = true, a.Downloads.Paths().Remote(incomplete)
	}
	if savePath != "" {
		prefs["save_path"] = a.Downloads.Paths().Remote(savePath)
	}
	warning := ""
	if len(prefs) > 0 {
		if err := a.Downloads.SetPrefs(ctx, prefs); err != nil {
			warning = "Saved in ReelStash, but qBittorrent could not be updated: " + err.Error()
		}
	}
	out := a.downloadSettingsView(ctx)
	out["warning"] = warning
	writeJSON(w, http.StatusOK, out)
}

// fixDownloadFolders resolves a folder warning: "apply-incomplete" pushes the
// protected folder to qBittorrent again; "protect-incomplete" adopts the
// folder qBittorrent already uses.
func (a *API) fixDownloadFolders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Fix string `json:"fix"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	s := downloads.LoadSettings(ctx, a.DB)
	switch req.Fix {
	case "apply-incomplete":
		if s.IncompleteDir == "" {
			writeError(w, http.StatusBadRequest, "no in-progress folder is set")
			return
		}
		if err := a.Downloads.SetPrefs(ctx, map[string]any{"temp_path_enabled": true, "temp_path": s.Paths().Remote(s.IncompleteDir)}); err != nil {
			downloadsError(w, err)
			return
		}
	case "protect-incomplete":
		prefs, err := a.Downloads.Prefs(ctx)
		if err != nil {
			downloadsError(w, err)
			return
		}
		dir := s.Paths().Local(qbit.Str(prefs, "temp_path"))
		if on, _ := prefs["temp_path_enabled"].(bool); !on || dir == "" {
			writeError(w, http.StatusBadRequest, "qBittorrent has no in-progress folder to protect")
			return
		}
		if err := downloads.CheckIncompleteDir(ctx, a.DB, dir); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := downloads.SetProtected(ctx, a.DB, dir); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to protect the folder")
			return
		}
		s.IncompleteDir = dir
		if err := downloads.SaveSettings(ctx, a.DB, s); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save settings")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "unknown fix "+req.Fix)
		return
	}
	writeJSON(w, http.StatusOK, a.downloadSettingsView(ctx))
}

func (a *API) listDownloadRules(w http.ResponseWriter, r *http.Request) {
	rules, err := downloads.ListRules(r.Context(), a.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load rules")
		return
	}
	if rules == nil {
		rules = []downloads.Rule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

// checkDownloadRule validates a rule's destination.
func (a *API) checkDownloadRule(w http.ResponseWriter, r *http.Request, rule *downloads.Rule) bool {
	if err := rule.Normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	dest, err := downloads.ResolveDir(r.Context(), a.DB, rule.DestStorageLocationID, rule.DestPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if pathguard.Load(r.Context(), a.DB).Locked(dest) {
		writeError(w, http.StatusBadRequest, "the destination can't be inside the in-progress folder")
		return false
	}
	return true
}

func (a *API) createDownloadRule(w http.ResponseWriter, r *http.Request) {
	var rule downloads.Rule
	if err := decodeJSON(r, &rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !a.checkDownloadRule(w, r, &rule) {
		return
	}
	saved, err := downloads.CreateRule(r.Context(), a.DB, rule)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the rule")
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (a *API) updateDownloadRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}
	var rule downloads.Rule
	if err := decodeJSON(r, &rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rule.ID = id
	if !a.checkDownloadRule(w, r, &rule) {
		return
	}
	saved, err := downloads.UpdateRule(r.Context(), a.DB, rule)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the rule")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a *API) deleteDownloadRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathInt64(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule id")
		return
	}
	if err := downloads.DeleteRule(r.Context(), a.DB, id); errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete the rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) reorderDownloadRules(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids are required")
		return
	}
	if err := downloads.ReorderRules(r.Context(), a.DB, req.IDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to reorder rules")
		return
	}
	a.listDownloadRules(w, r)
}

func (a *API) testDownloadRules(w http.ResponseWriter, r *http.Request) {
	plans, err := a.Downloads.TestRules(r.Context())
	if err != nil {
		downloadsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": plans})
}

func (a *API) downloadHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := downloads.History(r.Context(), a.DB, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	if items == nil {
		items = []downloads.HistoryEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) forgetDownload(w http.ResponseWriter, r *http.Request) {
	if err := a.Downloads.Forget(r.Context(), chi.URLParam(r, "hash")); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to forget the torrent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
