package httpapi

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reelstash/backend/internal/auth"
)

type fileRig struct {
	*harness
	mount string
	loc   int64
}

// newFileRig creates a storage location over a temp dir laid out as:
//
//	movies/a.mp4  movies/a.nfo  movies/.hidden  docs/note.txt  root.txt
func newFileRig(t *testing.T) fileRig {
	t.Helper()
	h := newHarness(t)
	mount := t.TempDir()
	for rel, body := range map[string]string{
		"movies/a.mp4": "video-bytes", "movies/a.nfo": "<movie/>", "movies/.hidden": "h",
		"docs/note.txt": "note", "root.txt": "r",
	} {
		p := filepath.Join(mount, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var loc int64
	if err := h.pool.QueryRow(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, uniq("files"), mount).Scan(&loc); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		h.pool.Exec(context.Background(), `DELETE FROM media_file WHERE storage_location_id = $1`, loc)
		h.pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, loc)
	})
	return fileRig{h, mount, loc}
}

func (f fileRig) q(path string, extra ...string) string {
	v := url.Values{"storageLocationId": {itoa(f.loc)}, "path": {path}}
	for i := 0; i+1 < len(extra); i += 2 {
		v.Set(extra[i], extra[i+1])
	}
	return v.Encode()
}

func (f fileRig) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(f.mount, filepath.FromSlash(rel)))
	return err == nil
}

// track registers movies/a.mp4 as an indexed media file.
func (f fileRig) track(rel string) int64 {
	f.t.Helper()
	item := f.newTitle(uniq("tracked"))
	var id int64
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size) VALUES ($1,$2,$3,$4,'mp4',1) RETURNING id`,
		item, f.loc, rel, filepath.Base(rel)).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f fileRig) relPath(id int64) (rel string, available bool) {
	f.pool.QueryRow(context.Background(), `SELECT relative_path, available FROM media_file WHERE id = $1`, id).Scan(&rel, &available)
	return
}

func names(items []map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, it := range items {
		out[it["name"].(string)] = true
	}
	return out
}

func TestFileListing(t *testing.T) {
	f := newFileRig(t)
	fileID := f.track("movies/a.mp4")

	root := f.items(f.do(&f.admin, "GET", "/api/files?"+f.q(""), nil))
	if n := names(root); !n["movies"] || !n["docs"] || !n["root.txt"] || len(n) != 3 {
		t.Errorf("root listing = %v", n)
	}
	movies := f.items(f.do(&f.admin, "GET", "/api/files?"+f.q("movies"), nil))
	n := names(movies)
	if !n["a.mp4"] || !n["a.nfo"] || n[".hidden"] {
		t.Errorf("dotfiles must be hidden: %v", n)
	}
	for _, it := range movies {
		if it["name"] == "a.mp4" {
			if int64(it["mediaFileId"].(float64)) != fileID || it["size"].(float64) != 11 || it["isDir"] != false {
				t.Errorf("indexed file entry = %v", it)
			}
		}
		if it["name"] == "a.nfo" && it["mediaFileId"] != nil {
			t.Error("unindexed file must not carry a mediaFileId")
		}
	}

	if rec := f.do(&f.admin, "GET", "/api/files?path=x", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no location: %d", rec.Code)
	}
	if rec := f.do(&f.admin, "GET", "/api/files?"+f.q("nope"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing dir: %d", rec.Code)
	}
	if rec := f.do(&f.admin, "GET", "/api/files?storageLocationId=999999999&path=", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown location: %d", rec.Code)
	}
}

// ".." can never climb out of the mount: the path is rooted before joining.
func TestFilePathTraversalStaysInsideMount(t *testing.T) {
	f := newFileRig(t)
	outside := filepath.Join(filepath.Dir(f.mount), "outside-secret.txt")
	os.WriteFile(outside, []byte("secret"), 0o644)
	t.Cleanup(func() { os.Remove(outside) })

	for _, p := range []string{"../outside-secret.txt", "../../etc/passwd", "movies/../../outside-secret.txt", "/../outside-secret.txt"} {
		rec := f.do(&f.admin, "GET", "/api/files/download?"+f.q(p), nil)
		if rec.Code == http.StatusOK {
			t.Errorf("download %q leaked a file outside the mount: %s", p, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("download %q leaked content", p)
		}
	}
	if rec := f.do(&f.admin, "GET", "/api/files?"+f.q("../.."), nil); rec.Code == http.StatusOK {
		// Rooting turns this into the mount's own root, never its parent.
		if _, ok := names(f.items(rec))["outside-secret.txt"]; ok {
			t.Error("listing escaped the mount")
		}
	}
	f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("../outside-secret.txt"), nil)
	if _, err := os.Stat(outside); err != nil {
		t.Error("delete with ../ removed a file outside the mount")
	}
}

func TestFileDownload(t *testing.T) {
	f := newFileRig(t)
	rec := f.do(&f.admin, "GET", "/api/files/download?"+f.q("movies/a.mp4"), nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "video-bytes" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "a.mp4") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("downloads must be nosniff")
	}
	if rec := f.do(&f.admin, "GET", "/api/files/download?"+f.q("movies"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("directory download: %d, want 404", rec.Code)
	}
	if rec := f.do(&f.admin, "GET", "/api/files/download?"+f.q(""), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("empty path: %d, want 400", rec.Code)
	}
	if rec := f.do(&f.admin, "GET", "/api/files/download?"+f.q("movies/none.mp4"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing file: %d", rec.Code)
	}
}

func TestFileDownloadRefusesSymlinkOutOfMount(t *testing.T) {
	f := newFileRig(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("top secret"), 0o644)
	if err := os.Symlink(outside, filepath.Join(f.mount, "link.txt")); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	rec := f.do(&f.admin, "GET", "/api/files/download?"+f.q("link.txt"), nil)
	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "top secret") {
		t.Errorf("symlink escaped the mount: %d %q", rec.Code, rec.Body.String())
	}
}

func TestFileDeleteMarksIndexedRowsMissing(t *testing.T) {
	f := newFileRig(t)
	fileID := f.track("movies/a.mp4")

	if rec := f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("movies"), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("folder without recursive: %d, want 400", rec.Code)
	}
	if !f.exists("movies/a.mp4") {
		t.Fatal("refused delete must not touch disk")
	}
	f.expect(f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("movies/a.mp4"), nil), 200)
	if f.exists("movies/a.mp4") {
		t.Error("file not deleted")
	}
	if _, avail := f.relPath(fileID); avail {
		t.Error("deleting the file should mark its row unavailable, not drop it")
	}

	other := f.track("docs/note.txt")
	f.expect(f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("docs", "recursive", "true"), nil), 200)
	if f.exists("docs") {
		t.Error("recursive delete left the folder")
	}
	if _, avail := f.relPath(other); avail {
		t.Error("rows under a deleted folder should be marked unavailable")
	}
	if rec := f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q("gone.txt"), nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing: %d", rec.Code)
	}
	if rec := f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q(""), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("delete empty path: %d", rec.Code)
	}
}

// Regression: "." (or "/", or "a/..") cleans to the mount root, and
// recursive=true used to remove the entire storage location.
func TestFileOperationsRefuseTheStorageRoot(t *testing.T) {
	f := newFileRig(t)
	for _, p := range []string{".", "/", "movies/..", "./", "docs/../."} {
		rec := f.do(&f.admin, "DELETE", "/api/admin/files?"+f.q(p, "recursive", "true"), nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("delete %q: %d, want 400", p, rec.Code)
		}
		rec = f.do(&f.admin, "PUT", "/api/admin/files", map[string]any{"storageLocationId": f.loc, "path": p, "newName": "renamed"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("rename %q: %d, want 400", p, rec.Code)
		}
		rec = f.do(&f.admin, "POST", "/api/admin/files/move", map[string]any{"storageLocationId": f.loc, "path": p, "destPath": "docs"})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("move %q: %d, want 400", p, rec.Code)
		}
	}
	if !f.exists("movies/a.mp4") || !f.exists("root.txt") || !f.exists("docs/note.txt") {
		t.Error("the storage root was damaged")
	}
	if _, err := os.Stat(f.mount); err != nil {
		t.Errorf("mount directory is gone: %v", err)
	}
}

func TestFileRename(t *testing.T) {
	f := newFileRig(t)
	fileID := f.track("movies/a.mp4")
	put := func(path, name string) *httptest.ResponseRecorder {
		return f.do(&f.admin, "PUT", "/api/admin/files", map[string]any{"storageLocationId": f.loc, "path": path, "newName": name})
	}

	for _, bad := range []string{"", "x/y", `x\y`} {
		if rec := put("movies/a.mp4", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("newName %q: %d, want 400", bad, rec.Code)
		}
	}
	if rec := put("movies/a.mp4", "a.nfo"); rec.Code != http.StatusConflict {
		t.Errorf("collision: %d, want 409", rec.Code)
	}
	if rec := put("movies/nope.mp4", "b.mp4"); rec.Code != http.StatusNotFound {
		t.Errorf("missing source: %d", rec.Code)
	}
	if rec := put("../a.mp4", "b.mp4"); rec.Code == http.StatusOK {
		t.Error("renaming outside the mount must not succeed")
	}

	out := f.jsonBody(put("movies/a.mp4", "b.mp4"), 200)
	if out["newPath"] != "movies/b.mp4" {
		t.Errorf("newPath = %v", out["newPath"])
	}
	if !f.exists("movies/b.mp4") || f.exists("movies/a.mp4") {
		t.Error("file not renamed on disk")
	}
	var name string
	f.pool.QueryRow(context.Background(), `SELECT file_name FROM media_file WHERE id = $1`, fileID).Scan(&name)
	if rel, _ := f.relPath(fileID); rel != "movies/b.mp4" || name != "b.mp4" {
		t.Errorf("row = %q / %q", rel, name)
	}

	// Renaming a folder rewrites every indexed path beneath it.
	f.expect(put("movies", "films"), 200)
	if rel, _ := f.relPath(fileID); rel != "films/b.mp4" {
		t.Errorf("row after folder rename = %q", rel)
	}
}

func TestFileMove(t *testing.T) {
	f := newFileRig(t)
	fileID := f.track("movies/a.mp4")
	move := func(path, dest string) *httptest.ResponseRecorder {
		return f.do(&f.admin, "POST", "/api/admin/files/move", map[string]any{"storageLocationId": f.loc, "path": path, "destPath": dest})
	}

	if rec := move("", "docs"); rec.Code != http.StatusBadRequest {
		t.Errorf("no path: %d", rec.Code)
	}
	if rec := move("movies/a.mp4", "nowhere"); rec.Code != http.StatusBadRequest {
		t.Errorf("missing destination: %d", rec.Code)
	}
	if rec := move("movies/a.mp4", "movies"); rec.Code != http.StatusBadRequest {
		t.Errorf("already there: %d", rec.Code)
	}
	if rec := move("movies/a.mp4", "root.txt"); rec.Code != http.StatusBadRequest {
		t.Errorf("destination is a file: %d", rec.Code)
	}
	os.WriteFile(filepath.Join(f.mount, "docs", "a.mp4"), []byte("other"), 0o644)
	if rec := move("movies/a.mp4", "docs"); rec.Code != http.StatusConflict {
		t.Errorf("name collision: %d, want 409", rec.Code)
	}
	os.Remove(filepath.Join(f.mount, "docs", "a.mp4"))

	// A folder can't go inside itself.
	os.MkdirAll(filepath.Join(f.mount, "movies", "sub"), 0o755)
	for _, dest := range []string{"movies", "movies/sub"} {
		if rec := move("movies", dest); rec.Code != http.StatusBadRequest {
			t.Errorf("move movies into %q: %d, want 400", dest, rec.Code)
		}
	}

	out := f.jsonBody(move("movies/a.mp4", "docs"), 200)
	if out["newPath"] != "docs/a.mp4" || !f.exists("docs/a.mp4") || f.exists("movies/a.mp4") {
		t.Errorf("move result = %v", out)
	}
	if rel, _ := f.relPath(fileID); rel != "docs/a.mp4" {
		t.Errorf("row = %q", rel)
	}
	// Folder moves rewrite children.
	f.expect(move("docs", "movies"), 200)
	if rel, _ := f.relPath(fileID); rel != "movies/docs/a.mp4" {
		t.Errorf("row after folder move = %q", rel)
	}
}

func TestFileMkdir(t *testing.T) {
	f := newFileRig(t)
	post := func(parent, name string) *httptest.ResponseRecorder {
		return f.do(&f.admin, "POST", "/api/admin/files/dir", map[string]any{"storageLocationId": f.loc, "path": parent, "name": name})
	}
	for _, bad := range []string{"", "a/b", `a\b`} {
		if rec := post("", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("name %q: %d, want 400", bad, rec.Code)
		}
	}
	if rec := post("", "docs"); rec.Code != http.StatusConflict {
		t.Errorf("existing: %d", rec.Code)
	}
	out := f.jsonBody(post("movies", "new"), 200)
	if out["path"] != "movies/new" || !f.exists("movies/new") {
		t.Errorf("mkdir = %v", out)
	}
	if rec := post("missing", "x"); rec.Code == http.StatusOK {
		t.Error("creating under a missing parent must fail")
	}
}

func multipartBody(t *testing.T, fields map[string]string, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		w.WriteField(k, v)
	}
	for name, content := range files {
		part, err := w.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		part.Write([]byte(content))
	}
	w.Close()
	return &buf, w.FormDataContentType()
}

func (f fileRig) upload(t *testing.T, who *actor, dir string, files map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, ctype := multipartBody(t, map[string]string{"storageLocationId": itoa(f.loc), "path": dir}, files)
	req := httptest.NewRequest("POST", "/api/admin/files/upload", body)
	req.Header.Set("Content-Type", ctype)
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: who.cookie})
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func TestFileUpload(t *testing.T) {
	f := newFileRig(t)

	out := f.jsonBody(f.upload(t, &f.admin, "docs", map[string]string{"new.txt": "hello", "other.txt": "world"}), 200)
	if up, _ := out["uploaded"].([]any); len(up) != 2 {
		t.Errorf("uploaded = %v", out["uploaded"])
	}
	if b, _ := os.ReadFile(filepath.Join(f.mount, "docs", "new.txt")); string(b) != "hello" {
		t.Errorf("content = %q", b)
	}

	// Existing names are never overwritten.
	rec := f.upload(t, &f.admin, "docs", map[string]string{"note.txt": "OVERWRITTEN"})
	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate upload: %d, want 409", rec.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(f.mount, "docs", "note.txt")); string(b) != "note" {
		t.Errorf("existing file was overwritten: %q", b)
	}

	// A hostile filename is reduced to its base name and stays in the folder.
	f.upload(t, &f.admin, "docs", map[string]string{"../../escape.txt": "x"})
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.mount), "escape.txt")); err == nil {
		t.Error("upload escaped the destination folder")
		os.Remove(filepath.Join(filepath.Dir(f.mount), "escape.txt"))
	}
	if !f.exists("docs/escape.txt") {
		t.Error("traversal filename should land as its base name in the target folder")
	}

	if rec := f.upload(t, &f.admin, "missing-dir", map[string]string{"a.txt": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("missing destination: %d", rec.Code)
	}
	if rec := f.upload(t, &f.admin, "docs", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no files: %d", rec.Code)
	}
	if rec := f.upload(t, &f.user, "docs", map[string]string{"u.txt": "x"}); rec.Code != http.StatusForbidden {
		t.Errorf("user upload: %d, want 403", rec.Code)
	}
	if f.exists("docs/u.txt") {
		t.Error("a non-admin wrote a file")
	}
}

func TestFileNFOReadAndWrite(t *testing.T) {
	f := newFileRig(t)
	out := f.jsonBody(f.do(&f.admin, "GET", "/api/files/nfo?"+f.q("movies/a.mp4"), nil), 200)
	if out["content"] != "<movie/>" {
		t.Errorf("nfo = %v", out)
	}
	out = f.jsonBody(f.do(&f.admin, "GET", "/api/files/nfo?"+f.q("docs/note.txt"), nil), 200)
	if out["content"] != "" {
		t.Errorf("no nfo should read as empty, got %v", out)
	}
	if rec := f.do(&f.admin, "GET", "/api/files/nfo?"+f.q(""), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("no path: %d", rec.Code)
	}

	f.expect(f.do(&f.admin, "PUT", "/api/admin/files/nfo", map[string]any{"storageLocationId": f.loc, "path": "movies/a.mp4", "content": "<movie><title>T</title></movie>"}), 200)
	if b, _ := os.ReadFile(filepath.Join(f.mount, "movies", "a.nfo")); string(b) != "<movie><title>T</title></movie>" {
		t.Errorf("nfo on disk = %q", b)
	}
	f.expect(f.do(&f.admin, "PUT", "/api/admin/files/nfo", map[string]any{"storageLocationId": f.loc, "path": "docs/note.txt", "content": "<movie/>"}), 200)
	if !f.exists("docs/note.nfo") {
		t.Error("a missing nfo should be created next to the media file")
	}
	if rec := f.do(&f.admin, "PUT", "/api/admin/files/nfo", map[string]any{"storageLocationId": f.loc, "content": "x"}); rec.Code != http.StatusBadRequest {
		t.Errorf("no path: %d", rec.Code)
	}
}

func TestFileLocationsListing(t *testing.T) {
	f := newFileRig(t)
	if !hasID(f.items(f.do(&f.admin, "GET", "/api/files/locations", nil)), f.loc) {
		t.Error("admin should see every storage location")
	}
	// A user with no library rooted here doesn't see it, and never sees mount paths.
	rec := f.do(&f.user, "GET", "/api/files/locations", nil)
	if hasID(f.items(rec), f.loc) {
		t.Error("user saw a location outside their libraries")
	}
	if strings.Contains(rec.Body.String(), f.mount) {
		t.Error("mount paths must not leak through the user endpoint")
	}
}

func TestFileCopy(t *testing.T) {
	f := newFileRig(t)
	copyTo := func(path, dest string) *httptest.ResponseRecorder {
		return f.do(&f.admin, "POST", "/api/admin/files/copy", map[string]any{"storageLocationId": f.loc, "path": path, "destPath": dest})
	}
	if rec := copyTo("", "docs"); rec.Code != http.StatusBadRequest {
		t.Errorf("no path: %d", rec.Code)
	}
	if rec := copyTo("movies/a.mp4", "nowhere"); rec.Code != http.StatusBadRequest {
		t.Errorf("missing destination: %d", rec.Code)
	}
	if rec := copyTo("movies", "movies"); rec.Code != http.StatusBadRequest {
		t.Errorf("folder into itself: %d", rec.Code)
	}

	out := f.jsonBody(copyTo("movies/a.mp4", "docs"), 200)
	if out["newPath"] != "docs/a.mp4" || !f.exists("docs/a.mp4") || !f.exists("movies/a.mp4") {
		t.Errorf("copy result = %v", out)
	}
	// Pasting again into the same folder never overwrites.
	out = f.jsonBody(copyTo("movies/a.mp4", "docs"), 200)
	if out["newPath"] != "docs/a copy.mp4" || !f.exists("docs/a copy.mp4") {
		t.Errorf("second copy = %v", out)
	}
	out = f.jsonBody(copyTo("movies/a.mp4", "docs"), 200)
	if out["newPath"] != "docs/a copy 2.mp4" {
		t.Errorf("third copy = %v", out)
	}
	// Folders copy recursively.
	out = f.jsonBody(copyTo("movies", "docs"), 200)
	if out["newPath"] != "docs/movies" || !f.exists("docs/movies/a.mp4") {
		t.Errorf("folder copy = %v", out)
	}
}

// TestFileTransferAcrossLocations covers paste between two storage locations
// (two disks): copy leaves the source, move takes the indexed rows along.
func TestFileTransferAcrossLocations(t *testing.T) {
	f := newFileRig(t)
	other := t.TempDir()
	os.MkdirAll(filepath.Join(other, "incoming"), 0o755)
	var loc2 int64
	if err := f.pool.QueryRow(context.Background(), `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, uniq("files2"), other).Scan(&loc2); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		f.pool.Exec(context.Background(), `DELETE FROM media_file WHERE storage_location_id = $1`, loc2)
		f.pool.Exec(context.Background(), `DELETE FROM storage_location WHERE id = $1`, loc2)
	})
	inOther := func(rel string) bool {
		_, err := os.Stat(filepath.Join(other, filepath.FromSlash(rel)))
		return err == nil
	}
	transfer := func(op, path, dest string) *httptest.ResponseRecorder {
		return f.do(&f.admin, "POST", "/api/admin/files/"+op, map[string]any{"storageLocationId": f.loc, "path": path, "destPath": dest, "destStorageLocationId": loc2})
	}

	if rec := transfer("copy", "movies/a.mp4", "nowhere"); rec.Code != http.StatusBadRequest {
		t.Errorf("missing destination: %d", rec.Code)
	}
	// ".." is clamped to the destination root, which has no "escape" folder.
	if rec := transfer("move", "docs/note.txt", "../escape"); rec.Code != http.StatusBadRequest || !f.exists("docs/note.txt") {
		t.Errorf("escape: %d", rec.Code)
	}

	out := f.jsonBody(transfer("copy", "movies/a.mp4", "incoming"), 200)
	if out["newPath"] != "incoming/a.mp4" || !inOther("incoming/a.mp4") || !f.exists("movies/a.mp4") {
		t.Errorf("cross copy = %v", out)
	}

	fileID := f.track("movies/a.mp4")
	os.Remove(filepath.Join(other, "incoming", "a.mp4"))
	out = f.jsonBody(transfer("move", "movies", "incoming"), 200)
	if out["newPath"] != "incoming/movies" || !inOther("incoming/movies/a.mp4") || f.exists("movies") {
		t.Errorf("cross move = %v", out)
	}
	var gotLoc int64
	var rel string
	f.pool.QueryRow(context.Background(), `SELECT storage_location_id, relative_path FROM media_file WHERE id = $1`, fileID).Scan(&gotLoc, &rel)
	if gotLoc != loc2 || rel != "incoming/movies/a.mp4" {
		t.Errorf("row = loc %d %q, want loc %d incoming/movies/a.mp4", gotLoc, rel, loc2)
	}
	os.WriteFile(filepath.Join(f.mount, "root2.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(other, "root2.txt"), nil, 0o644)
	if rec := f.do(&f.admin, "POST", "/api/admin/files/move", map[string]any{"storageLocationId": f.loc, "path": "root2.txt", "destPath": "", "destStorageLocationId": loc2}); rec.Code != http.StatusConflict {
		t.Errorf("cross move collision: %d, want 409", rec.Code)
	}
}

// TestCopyTreeKeepTimes checks the copy half of a cross-device move keeps
// modification times, so the scanner doesn't see a moved file as changed.
func TestCopyTreeKeepTimes(t *testing.T) {
	src := filepath.Join(t.TempDir(), "show")
	os.MkdirAll(filepath.Join(src, "s01"), 0o755)
	file := filepath.Join(src, "s01", "e01.mkv")
	os.WriteFile(file, []byte("x"), 0o644)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(file, old, old)

	dst := filepath.Join(t.TempDir(), "show")
	if err := copyTree(context.Background(), src, dst, true, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dst, "s01", "e01.mkv"))
	if err != nil || !info.ModTime().Equal(old) {
		t.Errorf("copied mtime = %v, %v; want %v", info, err, old)
	}
}
