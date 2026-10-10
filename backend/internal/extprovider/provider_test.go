package extprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reelstash/backend/internal/provider"
)

// fakePlugin is a minimal service speaking the contract.
func fakePlugin(t *testing.T, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		write(w, Manifest{ID: "mydb", Name: "My DB", Capabilities: Capabilities{Media: true, Performer: true, Series: true}, ImageHosts: []string{"img.mydb.test"}, Notice: "Data from My DB"})
	})
	mux.HandleFunc("/media/search", func(w http.ResponseWriter, r *http.Request) {
		var q map[string]string
		json.NewDecoder(r.Body).Decode(&q)
		write(w, map[string]any{"candidates": []wireCandidate{
			{Score: 250, AutoMatch: true, Result: wireMedia{ID: "m1", Title: "Found " + q["fileName"], Studio: "Acme", Performers: []string{"Ann"}, PerformerIDs: map[string]string{"Ann": "p1"}, Poster: "https://img.mydb.test/p.jpg", Kind: "episode", Show: "The Show", ShowID: "s1", Season: 2, Episode: 3, Crew: []wireCrew{{ID: "c1", Name: "Dee", Job: "director"}}}},
			{Score: -5, Result: wireMedia{ID: ""}}, // no id: dropped
		}})
	})
	mux.HandleFunc("/media/m1", func(w http.ResponseWriter, r *http.Request) { write(w, wireMedia{Title: "Detail"}) })
	mux.HandleFunc("/performers/search", func(w http.ResponseWriter, r *http.Request) {
		write(w, map[string]any{"results": []wirePerformer{{ID: "p1", Name: r.URL.Query().Get("q"), Image: "https://img.mydb.test/a.jpg"}}})
	})
	mux.HandleFunc("/studios/search", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "nope", http.StatusNotImplemented) })
	mux.HandleFunc("/series/s1", func(w http.ResponseWriter, r *http.Request) {
		write(w, wireSeries{ID: "s1", Title: "The Show", Parts: []wirePart{{ID: "m1", Number: 1, Title: "Pilot"}}})
	})
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
}

func TestDiscoverReadsAndValidatesTheManifest(t *testing.T) {
	srv := fakePlugin(t, "")
	defer srv.Close()
	spec, err := Discover(context.Background(), srv.URL+"/", "")
	if err != nil {
		t.Fatal(err)
	}
	if spec.ID != "x-mydb" || spec.Name != "My DB" || spec.BaseURL != srv.URL || !spec.Caps.Media || spec.Notice == "" || len(spec.ImageHosts) != 1 {
		t.Errorf("spec = %+v", spec)
	}
	for _, bad := range []string{"", "ftp://x", "not a url", "localhost:1"} {
		if _, err := Discover(context.Background(), bad, ""); err == nil {
			t.Errorf("Discover(%q) should fail", bad)
		}
	}
}

func TestDiscoverRejectsBadManifests(t *testing.T) {
	for name, m := range map[string]string{
		"bad id":          `{"id":"Bad ID!","name":"x","capabilities":{"media":true}}`,
		"no capabilities": `{"id":"ok","name":"x","capabilities":{}}`,
		"not json":        `<html>`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(m)) }))
		if _, err := Discover(context.Background(), srv.URL, ""); err == nil {
			t.Errorf("%s: should fail", name)
		}
		srv.Close()
	}
}

func TestTokenIsSentAndRefusalIsExplained(t *testing.T) {
	srv := fakePlugin(t, "s3cret")
	defer srv.Close()
	if _, err := Discover(context.Background(), srv.URL, ""); err == nil || !strings.Contains(err.Error(), "refused the access token") {
		t.Errorf("err = %v, want a token refusal", err)
	}
	spec, err := Discover(context.Background(), srv.URL, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	p := NewProvider(spec, "")
	if err := p.Test(context.Background()); err == nil {
		t.Error("Test without the token should fail")
	}
	p.Configure(map[string]string{"token": "s3cret"})
	if err := p.Test(context.Background()); err != nil {
		t.Errorf("Test with the token: %v", err)
	}
}

func TestProviderSpeaksTheContract(t *testing.T) {
	srv := fakePlugin(t, "")
	defer srv.Close()
	spec, _ := Discover(context.Background(), srv.URL, "")
	p := NewProvider(spec, "")
	ctx := context.Background()

	if p.ID() != "x-mydb" || !p.Enabled() || !p.Capabilities().Media || p.Capabilities().Studio {
		t.Fatalf("identity: %s %v %+v", p.ID(), p.Enabled(), p.Capabilities())
	}
	if p.(provider.Noticer).Notice() != "Data from My DB" {
		t.Error("notice not exposed")
	}

	cands, err := p.SearchMedia(ctx, provider.MediaQuery{FileName: "a.mkv"})
	if err != nil || len(cands) != 1 {
		t.Fatalf("cands = %v, err = %v (the candidate without an id must be dropped)", cands, err)
	}
	c := cands[0]
	if c.Score != 100 || !c.AutoMatch || c.Result.Title != "Found a.mkv" {
		t.Errorf("candidate = %+v", c)
	}
	r := c.Result
	if r.Ref != (provider.Ref{Provider: "x-mydb", ExternalID: "m1"}) || r.ShowRef.ExternalID != "s1" || r.Season != 2 || r.Episode != 3 ||
		r.PerformerRefs["Ann"].ExternalID != "p1" || len(r.Crew) != 1 || r.Crew[0].Ref.Provider != "x-mydb" {
		t.Errorf("result not mapped: %+v", r)
	}

	got, err := p.GetMedia(ctx, "m1")
	if err != nil || got.Ref.ExternalID != "m1" || got.Title != "Detail" {
		t.Errorf("GetMedia = %+v, %v (a missing id falls back to the requested one)", got, err)
	}
	if _, err := p.GetMedia(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v, want ErrNotFound", err)
	}
	perfs, err := p.SearchPerformers(ctx, "Ann")
	if err != nil || len(perfs) != 1 || perfs[0].Name != "Ann" {
		t.Errorf("performers = %v, %v", perfs, err)
	}
	if _, err := p.SearchStudios(ctx, "x"); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("studios: %v, want ErrUnsupported (capability not declared)", err)
	}

	sp, ok := p.(provider.SeriesProvider)
	if !ok {
		t.Fatal("a plugin that declares series must implement SeriesProvider")
	}
	series, err := sp.GetSeries(ctx, "s1")
	if err != nil || series.Title != "The Show" || len(series.Parts) != 1 || series.Parts[0].Ref.Provider != "x-mydb" {
		t.Errorf("series = %+v, %v", series, err)
	}
}

func TestUnreachableServiceGivesAReadableError(t *testing.T) {
	srv := fakePlugin(t, "")
	spec, _ := Discover(context.Background(), srv.URL, "")
	srv.Close()
	if err := NewProvider(spec, "").Test(context.Background()); err == nil || !strings.Contains(err.Error(), "My DB") {
		t.Errorf("err = %v, want one naming the provider", err)
	}
}

func TestRegistrationInTheRegistry(t *testing.T) {
	srv := fakePlugin(t, "")
	defer srv.Close()
	spec, _ := Discover(context.Background(), srv.URL, "")
	reg := provider.NewRegistry()
	reg.Register(NewProvider(spec, ""))
	if got := reg.Enabled(provider.KindMedia); len(got) != 1 || got[0].ID() != "x-mydb" {
		t.Errorf("enabled media providers = %v", got)
	}
	if !reg.ImageHostAllowed("img.mydb.test") || !reg.ImageHostAllowed("cdn.img.mydb.test") || reg.ImageHostAllowed("evil.test") {
		t.Error("image host allow-list does not follow the plugin's manifest")
	}
	if len(reg.EnabledSeries()) != 1 {
		t.Error("series support not visible to the registry")
	}
	if !reg.Unregister("x-mydb") || reg.Unregister("x-mydb") || len(reg.All()) != 0 {
		t.Error("Unregister")
	}
}

func TestDisabledPluginStaysConnectedButOutOfMatching(t *testing.T) {
	srv := fakePlugin(t, "")
	defer srv.Close()
	spec, _ := Discover(context.Background(), srv.URL, "")
	p := NewProvider(spec, "")
	reg := provider.NewRegistry()
	reg.Register(p)
	if len(reg.Enabled(provider.KindMedia)) != 1 {
		t.Fatal("a fresh plugin should be enabled")
	}
	p.(*seriesProvider).SetDisabled(true)
	if p.Enabled() || len(reg.Enabled(provider.KindMedia)) != 0 || len(reg.EnabledSeries()) != 0 {
		t.Error("a disabled plugin must not be used for matching")
	}
	if _, ok := reg.Get(spec.ID); !ok || p.(*seriesProvider).Spec().BaseURL == "" {
		t.Error("a disabled plugin stays registered, so links to it keep their name")
	}
	p.(*seriesProvider).SetDisabled(false)
	if !p.Enabled() {
		t.Error("re-enabling should work")
	}
}

func TestCallLogRecordsRequestsNewestFirst(t *testing.T) {
	srv := fakePlugin(t, "s3cr3t-value")
	defer srv.Close()
	spec, _ := Discover(context.Background(), srv.URL, "s3cr3t-value")
	ClearCalls(spec.ID)
	t.Cleanup(func() { ClearCalls(spec.ID) })
	p := NewProvider(spec, "s3cr3t-value")
	ctx := context.Background()
	p.SearchMedia(ctx, provider.MediaQuery{FileName: "a.mp4"})
	p.GetMedia(ctx, "zzz") // 404
	bad := NewProvider(spec, "wrong")
	bad.SearchPerformers(ctx, "Ann")

	calls := RecentCalls(spec.ID)
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	if calls[0].Path != "/performers/search?q=Ann" || calls[0].Status != 401 || calls[0].Error == "" {
		t.Errorf("newest = %+v", calls[0])
	}
	if calls[1].Status != 404 || calls[2].Method != "POST" || calls[2].Status != 200 || calls[2].Error != "" {
		t.Errorf("older = %+v / %+v", calls[1], calls[2])
	}
	for _, c := range calls {
		if strings.Contains(c.Path, "s3cr3t-value") || strings.Contains(c.Error, "s3cr3t-value") {
			t.Errorf("the token leaked into the log: %+v", c)
		}
	}
	if len(RecentCalls("probe")) != 0 {
		t.Error("probe calls made while connecting must not be logged")
	}
}

func TestCallLogIsBounded(t *testing.T) {
	ClearCalls("x-bound")
	t.Cleanup(func() { ClearCalls("x-bound") })
	for i := 0; i < maxCalls+30; i++ {
		recordCall("x-bound", Call{Path: "/p", Status: 200 + i})
	}
	calls := RecentCalls("x-bound")
	if len(calls) != maxCalls || calls[0].Status != 200+maxCalls+29 {
		t.Errorf("kept %d calls, newest status %d", len(calls), calls[0].Status)
	}
}

func TestPluginErrorMessagesAreShownPlainly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest" {
			json.NewEncoder(w).Encode(Manifest{ID: "msg", Name: "Msg", Capabilities: Capabilities{Media: true}})
			return
		}
		if r.URL.Path == "/media/a" {
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"error": "upstream said no"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("plain text failure"))
	}))
	defer srv.Close()
	spec, _ := Discover(context.Background(), srv.URL, "")
	p := NewProvider(spec, "")
	_, err := p.GetMedia(context.Background(), "a")
	if err == nil || err.Error() != "Msg answered HTTP 502: upstream said no" {
		t.Errorf("err = %v", err)
	}
	_, err = p.GetMedia(context.Background(), "b")
	if err == nil || !strings.Contains(err.Error(), "plain text failure") {
		t.Errorf("err = %v", err)
	}
}
