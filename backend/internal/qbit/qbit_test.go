package qbit_test

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha512"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reelstash/backend/internal/qbit"
	"reelstash/backend/internal/qbit/qbittest"
)

func TestClientSignsInAndRetriesExpiredSession(t *testing.T) {
	fake := qbittest.New()
	defer fake.Close()
	fake.AddTorrent("aaa", map[string]any{"name": "Movie"})
	c := qbit.New(qbit.Config{URL: fake.URL, Username: "admin", Password: "adminadmin"})
	ctx := context.Background()

	md, err := c.MainData(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !md.FullUpdate || len(md.Torrents) != 1 {
		t.Fatalf("maindata = %+v", md)
	}
	// qBittorrent forgets the session: the client signs in again once.
	before := fake.Logins
	fake.ExpireSessions()
	if _, err := c.Version(ctx); err != nil {
		t.Fatalf("after expiry: %v", err)
	}
	if fake.Logins != before+1 {
		t.Fatalf("logins = %d, want %d", fake.Logins, before+1)
	}
}

func TestClientReportsRefusedPassword(t *testing.T) {
	fake := qbittest.New()
	defer fake.Close()
	c := qbit.New(qbit.Config{URL: fake.URL, Username: "admin", Password: "wrong"})
	if _, err := c.Version(context.Background()); err != qbit.ErrLogin {
		t.Fatalf("err = %v, want ErrLogin", err)
	}
	// A refused sign-in is not retried right away (qBittorrent bans repeats).
	logins := fake.Logins
	_, _ = c.Version(context.Background())
	if fake.Logins != logins {
		t.Fatalf("signed in again during backoff")
	}
}

func TestClientSignsInToQBittorrent52(t *testing.T) {
	fake := qbittest.New()
	defer fake.Close()
	fake.Modern = true
	c := qbit.New(qbit.Config{URL: fake.URL, Username: "admin", Password: "adminadmin"})
	if _, err := c.Version(context.Background()); err != nil {
		t.Fatalf("204 sign-in: %v", err)
	}
	// The QBT_SID_8080 session cookie must be sent back.
	if _, err := c.MainData(context.Background(), 0); err != nil {
		t.Fatalf("request after sign-in: %v", err)
	}
	if _, err := qbit.New(qbit.Config{URL: fake.URL, Username: "admin", Password: "nope"}).Version(context.Background()); err != qbit.ErrLogin {
		t.Fatalf("401 sign-in: %v, want ErrLogin", err)
	}
}

func TestClientNotConfigured(t *testing.T) {
	if _, err := qbit.New(qbit.Config{}).Version(context.Background()); err != qbit.ErrNotConfigured {
		t.Fatalf("err = %v", err)
	}
}

func TestStopUsesPauseOnOldAPI(t *testing.T) {
	fake := qbittest.New()
	defer fake.Close()
	fake.APIVersion = "2.9.3"
	c := qbit.New(qbit.Config{URL: fake.URL, Username: "admin", Password: "adminadmin"})
	if err := c.Stop(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if got := fake.CallLog(); len(got) != 1 || got[0] != "torrents/pause a|b" {
		t.Fatalf("calls = %v", got)
	}
}

func TestAddSendsOptions(t *testing.T) {
	fake := qbittest.New()
	defer fake.Close()
	c := qbit.New(qbit.Config{URL: fake.URL, Username: "admin", Password: "adminadmin"})
	err := c.Add(context.Background(), []string{"magnet:?xt=1"}, []qbit.TorrentFile{{Name: "x.torrent", Data: []byte("d")}},
		qbit.AddOptions{SavePath: "/mnt/a", Category: "movies", Stopped: true})
	if err != nil {
		t.Fatal(err)
	}
	got := fake.Added[0]
	if got["urls"] != "magnet:?xt=1" || got["savepath"] != "/mnt/a" || got["category"] != "movies" || got["stopped"] != "true" || got["file"] != "x.torrent" || got["autoTMM"] != "false" {
		t.Fatalf("added = %v", got)
	}
}

func TestStateAppliesPartialUpdates(t *testing.T) {
	s := qbit.NewState()
	s.Apply(qbit.MainData{RID: 1, FullUpdate: true,
		Torrents:   map[string]map[string]any{"a": {"name": "A", "progress": 0.1}, "b": {"name": "B"}},
		Categories: map[string]map[string]any{"movies": {"savePath": "/m"}}, Tags: []string{"x"}})
	s.Apply(qbit.MainData{RID: 2, Torrents: map[string]map[string]any{"a": {"progress": 1.0}}, TorrentsRemoved: []string{"b"}, TagsRemoved: []string{"x"}})
	if len(s.Torrents) != 1 || s.Torrents["a"]["name"] != "A" || s.Torrents["a"]["progress"] != 1.0 || s.Torrents["a"]["hash"] != "a" {
		t.Fatalf("torrents = %v", s.Torrents)
	}
	if len(s.Tags) != 0 || s.RID != 2 || s.Categories["movies"]["savePath"] != "/m" {
		t.Fatalf("state = %+v", s)
	}
}

func TestTorrentComplete(t *testing.T) {
	done := qbit.View(map[string]any{"progress": 1.0, "completion_on": 5.0, "state": "stalledUP"})
	checking := qbit.View(map[string]any{"progress": 1.0, "completion_on": 5.0, "state": "checkingUP"})
	partial := qbit.View(map[string]any{"progress": 0.9, "completion_on": -1.0, "state": "downloading"})
	if !done.Complete() || checking.Complete() || partial.Complete() {
		t.Fatal("Complete() is wrong")
	}
}

func TestVersionAtLeast(t *testing.T) {
	for _, c := range []struct {
		have, want string
		ok         bool
	}{{"2.11.2", "2.11.0", true}, {"2.9.3", "2.11.0", false}, {"v5.0", "5.0.0", true}, {"2.11", "2.11.1", false}} {
		if got := qbit.VersionAtLeast(c.have, c.want); got != c.ok {
			t.Errorf("VersionAtLeast(%q, %q) = %v", c.have, c.want, got)
		}
	}
}

func TestPasswordHashMatchesQBittorrentFormat(t *testing.T) {
	salt := []byte("0123456789abcdef")
	got, err := qbit.PasswordHash("adminadmin", salt)
	if err != nil {
		t.Fatal(err)
	}
	key, _ := pbkdf2.Key(sha512.New, "adminadmin", salt, 100000, 64)
	want := "@ByteArray(" + base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(key) + ")"
	if got != want {
		t.Fatalf("hash = %s", got)
	}
}

func TestSeedConfigCreatesAndUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qBittorrent", "qBittorrent.conf")
	if err := qbit.SeedConfig(path, "admin", "short"); err == nil {
		t.Fatal("accepted a 5-character password")
	}
	if err := qbit.SeedConfig(path, "admin", "secret-one"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "[LegalNotice]\nAccepted=true") || !strings.Contains(string(raw), `WebUI\Username=admin`) || !strings.Contains(string(raw), `WebUI\Password_PBKDF2="@ByteArray(`) || !strings.Contains(string(raw), `WebUI\HostHeaderValidation=false`) {
		t.Fatalf("new config:\n%s", raw)
	}
	existing := "[BitTorrent]\nSession\\Port=6881\n\n[Preferences]\nWebUI\\Port=8080\nWebUI\\Username=old\n\n[Meta]\nx=1\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := qbit.SeedConfig(path, "reel", "secret-two"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	out := string(raw)
	if strings.Contains(out, "Username=old") || !strings.Contains(out, `WebUI\Username=reel`) || !strings.Contains(out, "Session\\Port=6881") || !strings.Contains(out, "WebUI\\Port=8080") || !strings.Contains(out, "[Meta]\nx=1") {
		t.Fatalf("updated config:\n%s", out)
	}
	if strings.Index(out, "Password_PBKDF2") > strings.Index(out, "[Meta]") {
		t.Fatalf("password written outside [Preferences]:\n%s", out)
	}
}
