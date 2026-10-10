package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/config"
)

// libraryFixture is one disk split into two libraries by folder ("movies/" and
// "private/"), a title in each, a performer in each, and a user who may only
// open the movies library.
type libraryFixture struct {
	pool                 *pgxpool.Pool
	handler              http.Handler
	mount                string
	disk                 int64
	movies, private      int64
	movieItem, privItem  int64
	movieActor, privPerf int64
	userCookie           string
	adminCookie          string
}

func newLibraryFixture(t *testing.T) libraryFixture {
	t.Helper()
	_, pool := unmatchAPI(t)
	ctx := context.Background()
	tag := uuid.NewString()[:8]
	f := libraryFixture{pool: pool, mount: t.TempDir()}
	for _, dir := range []string{"movies", "private", "other"} {
		if err := os.MkdirAll(filepath.Join(f.mount, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, rel := range []string{"movies/a.mp4", "private/b.mp4", "other/c.mp4"} {
		if err := os.WriteFile(filepath.Join(f.mount, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO storage_location (name, type, mount_path) VALUES ($1, 'LOCAL', $2) RETURNING id`, "disk-"+tag, f.mount).Scan(&f.disk))
	must(pool.QueryRow(ctx, `INSERT INTO library (name, mode) VALUES ($1, 'entertainment') RETURNING id`, "Movies "+tag).Scan(&f.movies))
	must(pool.QueryRow(ctx, `INSERT INTO library (name, mode) VALUES ($1, 'private') RETURNING id`, "Private "+tag).Scan(&f.private))
	_, err := pool.Exec(ctx, `INSERT INTO library_root (library_id, storage_location_id, path) VALUES ($1, $3, 'movies'), ($2, $3, 'private')`, f.movies, f.private, f.disk)
	must(err)

	item := func(title, rel string) int64 {
		var id int64
		must(pool.QueryRow(ctx, `INSERT INTO media_item (title) VALUES ($1) RETURNING id`, title+" "+tag).Scan(&id))
		_, err := pool.Exec(ctx, `INSERT INTO media_file (media_item_id, storage_location_id, relative_path, file_name, extension, file_size) VALUES ($1, $2, $3, $4, 'mp4', 1)`,
			id, f.disk, rel, filepath.Base(rel))
		must(err)
		return id
	}
	f.movieItem = item("Movie", "movies/a.mp4")
	f.privItem = item("Private", "private/b.mp4")
	performer := func(name string, mediaID int64) int64 {
		var id int64
		must(pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, name+" "+tag).Scan(&id))
		_, err := pool.Exec(ctx, `INSERT INTO media_item_performer (media_item_id, performer_id) VALUES ($1, $2)`, mediaID, id)
		must(err)
		return id
	}
	f.movieActor = performer("Actor", f.movieItem)
	f.privPerf = performer("Performer", f.privItem)

	session := func(role string) string {
		var uid int64
		must(pool.QueryRow(ctx, `INSERT INTO app_user (auth_subject, role) VALUES ($1, $2) RETURNING id`, role+"-"+uuid.NewString(), role).Scan(&uid))
		sid := uuid.NewString()
		_, err := pool.Exec(ctx, `INSERT INTO web_session (id, user_id, expires_at) VALUES ($1, $2, $3)`, sid, uid, time.Now().Add(time.Hour))
		must(err)
		if role == "USER" {
			_, err = pool.Exec(ctx, `INSERT INTO library_access (user_id, library_id) VALUES ($1, $2)`, uid, f.movies)
			must(err)
		}
		return sid
	}
	f.userCookie = session("USER")
	f.adminCookie = session("ADMIN")

	f.handler = NewRouter(pool, config.Config{PosterDir: t.TempDir()}, nil, nil, nil, nil, nil)
	t.Cleanup(func() {
		ctx := context.Background()
		pool.Exec(ctx, `DELETE FROM media_item WHERE id = ANY($1)`, []int64{f.movieItem, f.privItem})
		pool.Exec(ctx, `DELETE FROM performer WHERE id = ANY($1)`, []int64{f.movieActor, f.privPerf})
		pool.Exec(ctx, `DELETE FROM library WHERE id = ANY($1)`, []int64{f.movies, f.private})
		pool.Exec(ctx, `DELETE FROM storage_location WHERE id = $1`, f.disk)
	})
	return f
}

func (f libraryFixture) get(t *testing.T, cookie, path string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	return f.do(t, cookie, http.MethodGet, path, "", header...)
}

func (f libraryFixture) do(t *testing.T, cookie, method, path, body string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: auth.CookieName, Value: cookie})
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	return rec
}

func itemIDs(t *testing.T, rec *httptest.ResponseRecorder) map[int64]bool {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID int64 `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, it := range body.Items {
		ids[it.ID] = true
	}
	return ids
}

func TestLibraryAssignmentByFolder(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	lib := func(id int64) *int64 {
		var l *int64
		if err := f.pool.QueryRow(ctx, `SELECT library_id FROM media_item WHERE id = $1`, id).Scan(&l); err != nil {
			t.Fatal(err)
		}
		return l
	}
	if l := lib(f.movieItem); l == nil || *l != f.movies {
		t.Fatalf("movie title library = %v, want %d", l, f.movies)
	}
	if l := lib(f.privItem); l == nil || *l != f.private {
		t.Fatalf("private title library = %v, want %d", l, f.private)
	}
	// Moving the file follows it into the other library (trigger, no scan).
	if _, err := f.pool.Exec(ctx, `UPDATE media_file SET relative_path = 'private/a.mp4' WHERE media_item_id = $1`, f.movieItem); err != nil {
		t.Fatal(err)
	}
	if l := lib(f.movieItem); l == nil || *l != f.private {
		t.Fatalf("after move library = %v, want %d", l, f.private)
	}
	// Out of every library: unassigned.
	if _, err := f.pool.Exec(ctx, `UPDATE media_file SET relative_path = 'other/a.mp4' WHERE media_item_id = $1`, f.movieItem); err != nil {
		t.Fatal(err)
	}
	if l := lib(f.movieItem); l != nil {
		t.Fatalf("outside every library = %d, want NULL", *l)
	}
}

func TestLibraryAccessScoping(t *testing.T) {
	f := newLibraryFixture(t)
	movie, priv := strconv.FormatInt(f.movieItem, 10), strconv.FormatInt(f.privItem, 10)

	ids := itemIDs(t, f.get(t, f.userCookie, "/api/media/?limit=500"))
	if !ids[f.movieItem] || ids[f.privItem] {
		t.Fatalf("user media list = %v; want movie only", ids)
	}
	if rec := f.get(t, f.userCookie, "/api/media/"+priv); rec.Code != http.StatusNotFound {
		t.Fatalf("user GET private title = %d, want 404", rec.Code)
	}
	if rec := f.get(t, f.userCookie, "/api/media/"+priv+"/stream"); rec.Code != http.StatusNotFound {
		t.Fatalf("user stream private title = %d, want 404", rec.Code)
	}
	if rec := f.get(t, f.userCookie, "/api/media/"+movie); rec.Code != http.StatusOK {
		t.Fatalf("user GET movie = %d: %s", rec.Code, rec.Body.String())
	}
	// Selecting a library the user was not granted shows nothing.
	if ids := itemIDs(t, f.get(t, f.userCookie, "/api/media/?limit=500", "X-Library", strconv.FormatInt(f.private, 10))); len(ids) != 0 {
		t.Fatalf("user browsing ungranted library got %v", ids)
	}

	perfs := itemIDs(t, f.get(t, f.userCookie, "/api/performers/?limit=500"))
	if !perfs[f.movieActor] || perfs[f.privPerf] {
		t.Fatalf("user performer list = %v; want actor only", perfs)
	}
	if rec := f.get(t, f.userCookie, "/api/performers/"+strconv.FormatInt(f.privPerf, 10)); rec.Code != http.StatusNotFound {
		t.Fatalf("user GET private performer = %d, want 404", rec.Code)
	}

	// Admins see everything, or one library when browsing it.
	all := itemIDs(t, f.get(t, f.adminCookie, "/api/media/?limit=500"))
	if !all[f.movieItem] || !all[f.privItem] {
		t.Fatalf("admin media list missing titles: %v", all)
	}
	one := itemIDs(t, f.get(t, f.adminCookie, "/api/media/?limit=500", "X-Library", strconv.FormatInt(f.private, 10)))
	if one[f.movieItem] || !one[f.privItem] {
		t.Fatalf("admin browsing private library = %v", one)
	}

	libs := itemIDs(t, f.get(t, f.userCookie, "/api/libraries"))
	if !libs[f.movies] || libs[f.private] {
		t.Fatalf("user libraries = %v; want movies only", libs)
	}
}

func TestLibraryFileBrowserRoots(t *testing.T) {
	f := newLibraryFixture(t)
	disk := strconv.FormatInt(f.disk, 10)
	names := func(rec *httptest.ResponseRecorder) []string {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		var out []string
		for _, it := range body.Items {
			out = append(out, it.Name)
		}
		return out
	}
	if got := names(f.get(t, f.userCookie, "/api/files/?storageLocationId="+disk)); strings.Join(got, ",") != "movies" {
		t.Fatalf("user disk root lists %v, want only movies", got)
	}
	if got := names(f.get(t, f.userCookie, "/api/files/?storageLocationId="+disk+"&path=movies")); strings.Join(got, ",") != "a.mp4" {
		t.Fatalf("user movies folder lists %v", got)
	}
	if rec := f.get(t, f.userCookie, "/api/files/?storageLocationId="+disk+"&path=private"); rec.Code != http.StatusNotFound {
		t.Fatalf("user private folder = %d, want 404", rec.Code)
	}
	if rec := f.get(t, f.userCookie, "/api/files/download?storageLocationId="+disk+"&path=private/b.mp4"); rec.Code != http.StatusNotFound {
		t.Fatalf("user download private = %d, want 404", rec.Code)
	}
	if rec := f.get(t, f.userCookie, "/api/files/download?storageLocationId="+disk+"&path=movies/../private/b.mp4"); rec.Code != http.StatusNotFound {
		t.Fatalf("user traversal download = %d, want 404", rec.Code)
	}
	if got := names(f.get(t, f.adminCookie, "/api/files/?storageLocationId="+disk)); len(got) != 3 {
		t.Fatalf("admin disk root lists %v, want all three folders", got)
	}
}

func TestNonAdminCannotEditMetadata(t *testing.T) {
	f := newLibraryFixture(t)
	movie := strconv.FormatInt(f.movieItem, 10)
	if rec := f.do(t, f.userCookie, http.MethodPatch, "/api/media/"+movie, `{"title":"hacked"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("user title edit = %d, want 403", rec.Code)
	}
	if rec := f.do(t, f.userCookie, http.MethodPatch, "/api/media/"+movie, `{"favorite":true}`); rec.Code != http.StatusOK {
		t.Fatalf("user favorite = %d: %s", rec.Code, rec.Body.String())
	}
	var source string
	_ = f.pool.QueryRow(context.Background(), `SELECT metadata_source FROM media_item WHERE id = $1`, f.movieItem).Scan(&source)
	if source == "MANUAL" {
		t.Fatal("favoriting marked the title as manually edited")
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodPut, "/api/performers/" + strconv.FormatInt(f.movieActor, 10)},
		{http.MethodDelete, "/api/performers/" + strconv.FormatInt(f.movieActor, 10)},
		{http.MethodPut, "/api/media/" + movie + "/metadata"},
		{http.MethodPost, "/api/categories/"},
	} {
		if rec := f.do(t, f.userCookie, c.method, c.path, `{}`); rec.Code != http.StatusForbidden {
			t.Fatalf("user %s %s = %d, want 403", c.method, c.path, rec.Code)
		}
	}
}

func TestLibraryScopedPinLock(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	var uid int64
	if err := f.pool.QueryRow(ctx, `SELECT user_id FROM web_session WHERE id = $1`, f.userCookie).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	// The user can open both libraries; the PIN locks only the private one.
	if _, err := f.pool.Exec(ctx, `INSERT INTO library_access (user_id, library_id) VALUES ($1, $2)`, uid, f.private); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO app_lock (user_id, pin_hash, library_ids) VALUES ($1, 'x', $2)`, uid, []int64{f.private}); err != nil {
		t.Fatal(err)
	}
	invalidateLockCache(uid)
	priv := strconv.FormatInt(f.privItem, 10)

	ids := itemIDs(t, f.get(t, f.userCookie, "/api/media/?limit=500"))
	if !ids[f.movieItem] || ids[f.privItem] {
		t.Fatalf("locked user's media list = %v; want the movie only", ids)
	}
	if rec := f.get(t, f.userCookie, "/api/media/?limit=500", "X-Library", strconv.FormatInt(f.private, 10)); rec.Code != http.StatusLocked {
		t.Fatalf("browsing the locked library = %d, want 423", rec.Code)
	}
	if rec := f.get(t, f.userCookie, "/api/media/"+priv); rec.Code != http.StatusLocked {
		t.Fatalf("opening a locked title = %d, want 423", rec.Code)
	}
	var libs struct {
		Items []struct {
			ID     int64 `json:"id"`
			Locked bool  `json:"locked"`
		} `json:"items"`
	}
	rec := f.get(t, f.userCookie, "/api/libraries")
	_ = json.Unmarshal(rec.Body.Bytes(), &libs)
	found := false
	for _, l := range libs.Items {
		if l.ID == f.private {
			found = l.Locked
		}
	}
	if !found {
		t.Fatalf("switcher should list the private library as locked: %s", rec.Body.String())
	}
	// Status reports a library lock, not a whole-app one.
	rec = f.get(t, f.userCookie, "/api/applock")
	if !strings.Contains(rec.Body.String(), `"locked":false`) || !strings.Contains(rec.Body.String(), `"librariesLocked":true`) {
		t.Fatalf("applock status = %s", rec.Body.String())
	}
}

func TestEntertainmentListings(t *testing.T) {
	f := newLibraryFixture(t)
	ctx := context.Background()
	var showID, directorID int64
	if err := f.pool.QueryRow(ctx, `INSERT INTO series (name, kind) VALUES ($1, 'show') RETURNING id`, "Show "+uuid.NewString()[:8]).Scan(&showID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM series WHERE id = $1`, showID) })
	if _, err := f.pool.Exec(ctx, `UPDATE media_item SET kind = 'episode', season_number = 2, episode_number = 1, series_id = $2 WHERE id = $1`, f.movieItem, showID); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `INSERT INTO performer (name) VALUES ($1) RETURNING id`, "Director "+uuid.NewString()[:8]).Scan(&directorID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM performer WHERE id = $1`, directorID) })
	if _, err := f.pool.Exec(ctx, `INSERT INTO media_item_crew (media_item_id, performer_id, job) VALUES ($1, $2, 'director')`, f.movieItem, directorID); err != nil {
		t.Fatal(err)
	}
	lib := strconv.FormatInt(f.movies, 10)
	id := strconv.FormatInt

	if eps := itemIDs(t, f.get(t, f.userCookie, "/api/media/?kind=episode&season=2&seriesId="+id(showID, 10), "X-Library", lib)); !eps[f.movieItem] {
		t.Fatalf("season 2 episodes = %v", eps)
	}
	if movies := itemIDs(t, f.get(t, f.userCookie, "/api/media/?kind=movie", "X-Library", lib)); movies[f.movieItem] {
		t.Fatalf("an episode listed as a movie: %v", movies)
	}
	if shows := itemIDs(t, f.get(t, f.userCookie, "/api/series/?kind=show", "X-Library", lib)); !shows[showID] {
		t.Fatalf("shows = %v", shows)
	}
	if directors := itemIDs(t, f.get(t, f.userCookie, "/api/performers/?role=director", "X-Library", lib)); !directors[directorID] || directors[f.movieActor] {
		t.Fatalf("directors = %v", directors)
	}
	if rec := f.get(t, f.userCookie, "/api/performers/"+id(directorID, 10)); rec.Code != http.StatusOK {
		t.Fatalf("director detail = %d", rec.Code)
	}
	if titles := itemIDs(t, f.get(t, f.userCookie, "/api/media/?performerId="+id(directorID, 10))); !titles[f.movieItem] {
		t.Fatalf("director's titles = %v", titles)
	}
	rec := f.get(t, f.userCookie, "/api/series/"+id(showID, 10))
	if !strings.Contains(rec.Body.String(), `"seasons":[{"number":2,"episodeCount":1`) {
		t.Fatalf("show detail = %s", rec.Body.String())
	}
	rec = f.get(t, f.userCookie, "/api/media/"+id(f.movieItem, 10))
	if !strings.Contains(rec.Body.String(), `"job":"director"`) || !strings.Contains(rec.Body.String(), `"seasonNumber":2`) {
		t.Fatalf("title detail missing crew/season: %s", rec.Body.String())
	}
}

func TestFavoritesAndPlaylistsArePerUser(t *testing.T) {
	f := newLibraryFixture(t)
	movie := strconv.FormatInt(f.movieItem, 10)
	actor := strconv.FormatInt(f.movieActor, 10)
	// The admin can open the movie too; the two users act independently.
	if rec := f.do(t, f.userCookie, http.MethodPatch, "/api/media/"+movie, `{"favorite":true}`); rec.Code != http.StatusOK {
		t.Fatalf("favorite = %d", rec.Code)
	}
	if ids := itemIDs(t, f.get(t, f.userCookie, "/api/media/?favorite=true&limit=500")); !ids[f.movieItem] {
		t.Fatalf("user's favorites missing the movie: %v", ids)
	}
	if ids := itemIDs(t, f.get(t, f.adminCookie, "/api/media/?favorite=true&limit=500")); ids[f.movieItem] {
		t.Fatal("one user's favorite shows up in another user's favorites")
	}
	if rec := f.get(t, f.adminCookie, "/api/media/"+movie); !strings.Contains(rec.Body.String(), `"favorite":false`) {
		t.Fatalf("admin sees the user's favorite flag: %s", rec.Body.String())
	}

	if rec := f.do(t, f.userCookie, http.MethodPut, "/api/performers/"+actor+"/favorite", `{"favorite":true}`); rec.Code != http.StatusOK {
		t.Fatalf("performer favorite = %d: %s", rec.Code, rec.Body.String())
	}
	if ids := itemIDs(t, f.get(t, f.userCookie, "/api/performers/?favorite=true&limit=500")); !ids[f.movieActor] {
		t.Fatalf("user's favorite performers = %v", ids)
	}
	if ids := itemIDs(t, f.get(t, f.adminCookie, "/api/performers/?favorite=true&limit=500")); ids[f.movieActor] {
		t.Fatal("one user's favorite performer shows up for another user")
	}

	rec := f.do(t, f.userCookie, http.MethodPost, "/api/collections/", `{"name":"Mine `+uuid.NewString()[:8]+`"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create playlist = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM collection WHERE id = $1`, created.ID) })
	pl := strconv.FormatInt(created.ID, 10)
	if rec := f.do(t, f.userCookie, http.MethodPost, "/api/collections/"+pl+"/items", `{"mediaItemId":`+movie+`}`); rec.Code >= 300 {
		t.Fatalf("add to own playlist = %d: %s", rec.Code, rec.Body.String())
	}
	if ids := itemIDs(t, f.get(t, f.userCookie, "/api/collections/")); !ids[created.ID] {
		t.Fatalf("owner's playlists = %v", ids)
	}
	if ids := itemIDs(t, f.get(t, f.adminCookie, "/api/collections/")); ids[created.ID] {
		t.Fatal("another user's playlist is listed")
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/collections/" + pl, ""},
		{http.MethodDelete, "/api/collections/" + pl, ""},
		{http.MethodPost, "/api/collections/" + pl + "/items", `{"mediaItemId":` + movie + `}`},
	} {
		if rec := f.do(t, f.adminCookie, c.method, c.path, c.body); rec.Code != http.StatusNotFound {
			t.Fatalf("other user %s %s = %d, want 404", c.method, c.path, rec.Code)
		}
	}
}
