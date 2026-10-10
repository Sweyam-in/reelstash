package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"1.0.1", "1.0.0", true}, {"1.0.0", "1.0.0", false}, {"1.0.0", "1.0.1", false},
		{"1.10.0", "1.9.9", true}, {"2.0.0", "1.99.99", true}, {"v1.2.0", "1.1.0", true},
		{"1.0.0-beta", "1.0.0", true}, // not major.minor.patch: any difference counts as newer
	} {
		if got := newer(tc.a, tc.b); got != tc.want {
			t.Errorf("newer(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func versioned(version string) []byte {
	return zipOfBytes(map[string]string{ManifestFile: `{"id":"upd","name":"Updatable","version":"` + version + `","kind":"provider","run":{"command":["helper"]},
		"config":[{"key":"mode","label":"Mode","env":"HELPER_MODE"}]}`})
}

// publisher serves an update index and the zips it lists.
type publisher struct {
	srv      *httptest.Server
	releases map[string][]byte // version -> zip
	latest   string
	badSum   bool
	hits     int
}

func newPublisher(t *testing.T, versions ...string) *publisher {
	t.Helper()
	p := &publisher{releases: map[string][]byte{}}
	for _, v := range versions {
		p.releases[v] = versioned(v)
		p.latest = v
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/dist/index.json", func(w http.ResponseWriter, r *http.Request) {
		p.hits++
		var rels []Release
		for v, data := range p.releases {
			sum := sha256.Sum256(data)
			hexSum := hex.EncodeToString(sum[:])
			if p.badSum {
				hexSum = strings.Repeat("0", 64)
			}
			rels = append(rels, Release{Version: v, File: "upd/upd-" + v + ".zip", SHA256: hexSum, Size: int64(len(data))})
		}
		json.NewEncoder(w).Encode(index{Plugins: []IndexEntry{{ID: "upd", Name: "Updatable", Kind: KindProvider, Latest: p.latest, Versions: rels}}})
	})
	mux.HandleFunc("/dist/upd/", func(w http.ResponseWriter, r *http.Request) {
		v := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/dist/upd/upd-"), ".zip")
		if data, ok := p.releases[v]; ok {
			w.Write(data)
			return
		}
		http.NotFound(w, r)
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *publisher) index() string { return p.srv.URL + "/dist/index.json" }

func TestCheckAndApplyAnUpdate(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	pub := newPublisher(t, "1.0.0", "1.1.0")

	if _, err := r.m.InstallZip(ctx, versioned("1.0.0")); err != nil {
		t.Fatal(err)
	}
	r.waitStatus(t, "upd", StatusRunning)
	if err := r.m.SetConfig(ctx, "upd", map[string]string{"mode": "kept"}); err != nil {
		t.Fatal(err)
	}
	r.waitStatus(t, "upd", StatusRunning)

	// No source yet.
	if _, err := r.m.CheckUpdate(ctx, "upd"); err == nil || !strings.Contains(err.Error(), "no update source") {
		t.Errorf("without a source: %v", err)
	}
	for _, bad := range []string{"ftp://x/index.json", "not a url", "http://"} {
		if err := r.m.SetUpdateSource(ctx, "upd", bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
	if err := r.m.SetUpdateSource(ctx, "upd", pub.index()); err != nil {
		t.Fatal(err)
	}
	got, err := r.m.CheckUpdate(ctx, "upd")
	if err != nil || !got.Available || got.Current != "1.0.0" || got.Latest != "1.1.0" {
		t.Fatalf("check = %+v, %v", got, err)
	}

	// A download that does not match its checksum is never installed.
	pub.badSum = true
	if _, err := r.m.Update(ctx, "upd"); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Errorf("a corrupt download: %v", err)
	}
	if in, _ := r.m.Get(ctx, "upd"); in.Version != "1.0.0" {
		t.Errorf("a refused update changed the version to %s", in.Version)
	}
	pub.badSum = false

	in, err := r.m.Update(ctx, "upd")
	if err != nil || in.Version != "1.1.0" || in.Previous != "1.0.0" {
		t.Fatalf("update = %+v, %v", in, err)
	}
	if cfg := r.m.Config(ctx, "upd"); cfg["mode"] != "kept" {
		t.Errorf("settings lost in the update: %v", cfg)
	}
	if src := r.m.UpdateSource(ctx, "upd"); src != pub.index() {
		t.Errorf("update source lost in the update: %q", src)
	}
	r.waitStatus(t, "upd", StatusRunning)

	// Now current: nothing to do.
	if got, _ := r.m.CheckUpdate(ctx, "upd"); got.Available {
		t.Errorf("after updating: %+v", got)
	}
	if _, err := r.m.Update(ctx, "upd"); err == nil || !strings.Contains(err.Error(), "already on the latest") {
		t.Errorf("updating twice: %v", err)
	}
}

func TestUpdateSourceFromThePluginManifest(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	pub := newPublisher(t, "1.0.0", "1.2.0")
	zip := zipOfBytes(map[string]string{ManifestFile: `{"id":"upd","version":"1.0.0","kind":"provider","updateUrl":"` + pub.index() + `","run":{"command":["helper"]}}`})
	in, err := r.m.InstallZip(ctx, zip)
	if err != nil || in.UpdateURL != pub.index() {
		t.Fatalf("install = %+v, %v", in, err)
	}
	if got, err := r.m.CheckUpdate(ctx, "upd"); err != nil || got.Latest != "1.2.0" {
		t.Errorf("check with the manifest's source: %+v, %v", got, err)
	}
	// An address set by the admin wins, and clearing it falls back to the manifest's.
	r.m.SetUpdateSource(ctx, "upd", "http://127.0.0.1:1/other.json")
	if r.m.UpdateSource(ctx, "upd") != "http://127.0.0.1:1/other.json" {
		t.Error("the saved source must win")
	}
	r.m.SetUpdateSource(ctx, "upd", "")
	if r.m.UpdateSource(ctx, "upd") != pub.index() {
		t.Error("clearing it must fall back to plugin.json")
	}
}

func TestUpdateSourceProblemsAreReported(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.m.InstallZip(ctx, versioned("1.0.0")); err != nil {
		t.Fatal(err)
	}
	notIndex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) }))
	defer notIndex.Close()
	r.m.SetUpdateSource(ctx, "upd", notIndex.URL)
	if _, err := r.m.CheckUpdate(ctx, "upd"); err == nil || !strings.Contains(err.Error(), "not a plugin index") {
		t.Errorf("a page that is not an index: %v", err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"plugins":[{"id":"someone-else","latest":"1.0.0","versions":[{"version":"1.0.0","file":"x.zip"}]}]}`))
	}))
	defer other.Close()
	r.m.SetUpdateSource(ctx, "upd", other.URL)
	if _, err := r.m.CheckUpdate(ctx, "upd"); err == nil || !strings.Contains(err.Error(), "does not list") {
		t.Errorf("an index without this plugin: %v", err)
	}
	if _, err := r.m.CheckUpdate(ctx, "nope"); err != ErrNotFound {
		t.Errorf("unknown plugin: %v", err)
	}
}

func TestInstallingANewerZipReportsTheUpgrade(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	first, err := r.m.InstallZip(ctx, versioned("1.0.0"))
	if err != nil || first.Previous != "" {
		t.Fatalf("first install = %+v, %v", first, err)
	}
	up, err := r.m.InstallZip(ctx, versioned("1.0.1"))
	if err != nil || up.Previous != "1.0.0" || up.Version != "1.0.1" {
		t.Errorf("upgrade = %+v, %v", up, err)
	}
}
