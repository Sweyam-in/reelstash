package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// localWebHandler serves the two built Vite apps beside the API for local packages.
func localWebHandler(api http.Handler, dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/auth/") || path == "/healthz" {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			api.ServeHTTP(w, r)
			return
		}
		entry := "index.html"
		switch {
		case path == "/admin" || strings.HasPrefix(path, "/admin/"):
			entry = "admin.html"
		case path == "/files" || strings.HasPrefix(path, "/files/"):
			entry = "files.html"
		case path == "/downloads" || strings.HasPrefix(path, "/downloads/"):
			entry = "downloads.html"
		case path != "/":
			if info, err := os.Stat(filepath.Join(dir, filepath.Clean(strings.TrimPrefix(path, "/")))); err == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(dir, entry))
	})
}
