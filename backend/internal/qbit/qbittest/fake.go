// Package qbittest is an in-memory qBittorrent WebUI for tests.
package qbittest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
)

// Server is a fake qBittorrent. Torrents are field maps as maindata reports
// them; SetLocation moves a torrent at once (or leaves it "moving" while
// HoldMoves is set).
type Server struct {
	*httptest.Server
	Username, Password string
	APIVersion         string

	mu         sync.Mutex
	rid        int64
	Torrents   map[string]map[string]any
	FileNames  map[string][]string
	Categories map[string]map[string]any
	Tags       map[string]bool
	Prefs      map[string]any
	Calls      []string
	Added      []map[string]string
	HoldMoves  bool
	// Modern answers sign-in like qBittorrent 5.2: 204 on success, 401 on refusal.
	Modern    bool
	FailMoves bool
	Logins    int
	sid       string
}

// cookieName is the session cookie: QBT_SID_<port> since qBittorrent 5.2.
func (s *Server) cookieName() string {
	if s.Modern {
		return "QBT_SID_8080"
	}
	return "SID"
}

// ExpireSessions forgets every session, as a qBittorrent restart does.
func (s *Server) ExpireSessions() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sid = ""
}

// New starts a fake with one admin/adminadmin account.
func New() *Server {
	s := &Server{
		Username: "admin", Password: "adminadmin", APIVersion: "2.11.2",
		Torrents: map[string]map[string]any{}, FileNames: map[string][]string{},
		Categories: map[string]map[string]any{}, Tags: map[string]bool{},
		Prefs: map[string]any{"save_path": "/mnt/downloads", "temp_path_enabled": false, "temp_path": "", "dl_limit": float64(0)},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// AddTorrent adds a torrent; fields override the defaults.
func (s *Server) AddTorrent(hash string, fields map[string]any, files ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := map[string]any{"name": hash, "state": "downloading", "progress": 0.5, "size": float64(1 << 30),
		"save_path": s.Prefs["save_path"], "category": "", "tags": "", "completion_on": float64(-1), "added_on": float64(1)}
	for k, v := range fields {
		t[k] = v
	}
	if _, ok := t["content_path"]; !ok {
		t["content_path"] = path.Join(t["save_path"].(string), t["name"].(string))
	}
	s.Torrents[hash] = t
	s.FileNames[hash] = files
}

// Update changes a torrent's fields.
func (s *Server) Update(hash string, fields map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range fields {
		s.Torrents[hash][k] = v
	}
}

// Torrent copies a torrent's fields.
func (s *Server) Torrent(hash string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]any{}
	for k, v := range s.Torrents[hash] {
		out[k] = v
	}
	return out
}

// CallLog copies the actions received.
func (s *Server) CallLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Calls...)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	route := strings.TrimPrefix(r.URL.Path, "/api/v2/")
	if route == "auth/login" {
		_ = r.ParseForm()
		s.Logins++
		if r.FormValue("username") == s.Username && r.FormValue("password") == s.Password {
			s.sid = "sid-" + strings.Repeat("x", s.Logins)
			http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: s.sid})
			if s.Modern {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = w.Write([]byte("Ok."))
			return
		}
		if s.Modern {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("Fails."))
		return
	}
	if c, err := r.Cookie(s.cookieName()); err != nil || s.sid == "" || c.Value != s.sid {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		_ = r.ParseMultipartForm(1 << 20)
	} else {
		_ = r.ParseForm()
	}
	hashes := strings.Split(r.FormValue("hashes"), "|")
	writeJSON := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	switch route {
	case "app/version":
		_, _ = w.Write([]byte("v5.0.2"))
	case "app/webapiVersion":
		_, _ = w.Write([]byte(s.APIVersion))
	case "app/preferences":
		writeJSON(s.Prefs)
	case "app/setPreferences":
		var p map[string]any
		if err := json.Unmarshal([]byte(r.FormValue("json")), &p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for k, v := range p {
			s.Prefs[k] = v
		}
		s.Calls = append(s.Calls, "setPreferences")
	case "sync/maindata":
		s.rid++
		tags := []string{}
		for t := range s.Tags {
			tags = append(tags, t)
		}
		writeJSON(map[string]any{"rid": s.rid, "full_update": true, "torrents": s.Torrents, "categories": s.Categories,
			"tags": tags, "server_state": map[string]any{"dl_info_speed": 10, "connection_status": "connected"}})
	case "torrents/files":
		files := []map[string]any{}
		for i, n := range s.FileNames[r.FormValue("hash")] {
			files = append(files, map[string]any{"index": i, "name": n, "size": 1, "progress": 1, "priority": 1})
		}
		writeJSON(files)
	case "torrents/properties":
		writeJSON(map[string]any{"save_path": s.Torrents[r.FormValue("hash")]["save_path"]})
	case "torrents/trackers":
		writeJSON([]map[string]any{})
	case "torrents/setLocation":
		s.Calls = append(s.Calls, "setLocation "+r.FormValue("location"))
		if s.FailMoves {
			w.WriteHeader(http.StatusConflict)
			return
		}
		for _, h := range hashes {
			t := s.Torrents[h]
			if t == nil {
				continue
			}
			if s.HoldMoves {
				t["state"] = "moving"
				continue
			}
			t["save_path"] = r.FormValue("location")
			t["content_path"] = path.Join(r.FormValue("location"), t["name"].(string))
		}
	case "torrents/delete":
		s.Calls = append(s.Calls, "delete "+r.FormValue("hashes")+" files="+r.FormValue("deleteFiles"))
		for _, h := range hashes {
			delete(s.Torrents, h)
		}
	case "torrents/addTags":
		s.Calls = append(s.Calls, "addTags "+r.FormValue("tags"))
		for _, h := range hashes {
			if t := s.Torrents[h]; t != nil {
				t["tags"] = strings.Trim(t["tags"].(string)+","+r.FormValue("tags"), ",")
			}
		}
	case "torrents/add":
		entry := map[string]string{}
		for k, v := range r.MultipartForm.Value {
			entry[k] = v[0]
		}
		for _, fh := range r.MultipartForm.File["torrents"] {
			entry["file"] = fh.Filename
		}
		s.Added = append(s.Added, entry)
		_, _ = w.Write([]byte("Ok."))
	case "torrents/stop", "torrents/start", "torrents/pause", "torrents/resume", "torrents/recheck", "torrents/setCategory",
		"torrents/createCategory", "torrents/removeCategories", "torrents/createTags", "torrents/deleteTags", "transfer/toggleSpeedLimitsMode":
		s.Calls = append(s.Calls, route+" "+r.FormValue("hashes")+r.FormValue("category"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// Complete marks a torrent finished at unix time at.
func (s *Server) Complete(hash string, at int64) {
	s.Update(hash, map[string]any{"progress": 1.0, "state": "stalledUP", "completion_on": float64(at)})
}

// FinishMove ends a held move at the destination.
func (s *Server) FinishMove(hash, location string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.Torrents[hash]
	t["state"], t["save_path"] = "stalledUP", location
	t["content_path"] = path.Join(location, t["name"].(string))
}
