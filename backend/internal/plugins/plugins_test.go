package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/provider"
)

// TestHelperPlugin is not a test: when ReelStash runs the test binary as a
// bundle (see newRig), it serves the plugin contract on $PORT.
func TestHelperPlugin(t *testing.T) {
	token := os.Getenv("REELSTASH_PLUGIN_TOKEN")
	if token == "" {
		return
	}
	data := os.Getenv("REELSTASH_PLUGIN_DATA")
	os.WriteFile(filepath.Join(data, "env.txt"), []byte(strings.Join(os.Environ(), "\n")), 0o644)
	if os.Getenv("HELPER_MODE") == "crash" {
		os.Exit(3)
	}
	id := os.Getenv("REELSTASH_PLUGIN_ID")
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if os.Getenv("REELSTASH_PLUGIN_KIND") == "auth" {
			json.NewEncoder(w).Encode(authplugin.Manifest{ID: id, Name: "Helper Auth", Login: authplugin.Login{Type: authplugin.LoginRedirect}})
			return
		}
		mid := id
		if os.Getenv("HELPER_MODE") == "wrong-id" {
			mid = "other"
		}
		json.NewEncoder(w).Encode(extprovider.Manifest{ID: mid, Name: "Helper", Capabilities: extprovider.Capabilities{Media: true}})
	})
	http.ListenAndServe(os.Getenv("HOST")+":"+os.Getenv("PORT"), mux)
}

type rig struct {
	m    *Manager
	reg  *provider.Registry
	auth *authplugin.Registry
	root string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{reg: provider.NewRegistry(), auth: authplugin.NewRegistry(), root: t.TempDir()}
	r.m = NewManager(Options{Root: r.root, Enabled: true, Store: NewMemStore(), Providers: r.reg, Auth: r.auth})
	r.m.commandHook = func(argv []string) []string {
		return []string{os.Args[0], "-test.run=^TestHelperPlugin$", "-test.v=false"}
	}
	r.m.retryDelay = func(int) time.Duration { return 20 * time.Millisecond }
	ctx, cancel := context.WithCancel(context.Background())
	r.m.StartAll(ctx)
	t.Cleanup(func() { cancel(); r.m.Shutdown() })
	return r
}

func helperBundle(id, kind string, extra string) fstest.MapFS {
	return fstest.MapFS{ManifestFile: {Data: []byte(`{"id":"` + id + `","name":"Helper ` + id + `","version":"1.0.0","kind":"` + kind + `",
		"run":{"command":["helper"]},"config":[{"key":"mode","label":"Mode","env":"HELPER_MODE"}` + extra + `]}`)}}
}

func (r *rig) waitStatus(t *testing.T, id, want string) Info {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var in Info
	for time.Now().Before(deadline) {
		in, _ = r.m.Get(context.Background(), id)
		if in.Status == want {
			return in
		}
		time.Sleep(25 * time.Millisecond)
	}
	logs, _ := r.m.Logs(id)
	t.Fatalf("bundle %s: status %q (error %q), want %q\nlog:\n%s", id, in.Status, in.Error, want, strings.Join(logs, "\n"))
	return in
}

func TestParseManifest(t *testing.T) {
	good := `{"id":"my-plugin","name":"My","version":"1.2","kind":"provider","run":{"command":["python3","server.py"]},
		"config":[{"key":"apiKey","label":"Key","secret":true,"required":true}]}`
	m, err := ParseManifest([]byte(good))
	if err != nil || m.Config[0].EnvName() != "APIKEY" {
		t.Fatalf("good manifest: %+v, %v", m, err)
	}
	for name, raw := range map[string]string{
		"not json":       `{`,
		"bad id":         `{"id":"My Plugin","version":"1","kind":"auth","run":{"command":["x"]}}`,
		"no version":     `{"id":"ab","kind":"auth","run":{"command":["x"]}}`,
		"bad kind":       `{"id":"ab","version":"1","kind":"theme","run":{"command":["x"]}}`,
		"no command":     `{"id":"ab","version":"1","kind":"auth","run":{}}`,
		"absolute cmd":   `{"id":"ab","version":"1","kind":"auth","run":{"command":["/bin/sh"]}}`,
		"escaping cmd":   `{"id":"ab","version":"1","kind":"auth","run":{"command":["./../x"]}}`,
		"reserved env":   `{"id":"ab","version":"1","kind":"auth","run":{"command":["x"]},"config":[{"key":"a","env":"PATH"}]}`,
		"reserved rs":    `{"id":"ab","version":"1","kind":"auth","run":{"command":["x"]},"config":[{"key":"a","env":"REELSTASH_PLUGIN_TOKEN"}]}`,
		"loader env":     `{"id":"ab","version":"1","kind":"auth","run":{"command":["x"]},"config":[{"key":"a","env":"LD_PRELOAD"}]}`,
		"duplicate key":  `{"id":"ab","version":"1","kind":"auth","run":{"command":["x"]},"config":[{"key":"a"},{"key":"a","env":"B"}]}`,
		"bad config key": `{"id":"ab","version":"1","kind":"auth","run":{"command":["x"]},"config":[{"key":"a b"}]}`,
	} {
		if _, err := ParseManifest([]byte(raw)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if miss := m.MissingRequired(map[string]string{}); len(miss) != 1 || miss[0] != "apiKey" {
		t.Errorf("missing = %v", miss)
	}
	if miss := m.MissingRequired(map[string]string{"apiKey": " x "}); len(miss) != 0 {
		t.Errorf("missing with a value = %v", miss)
	}
}

func zipOf(t *testing.T, files map[string]string, modes map[string]os.FileMode) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		if mode, ok := modes[name]; ok {
			h.SetMode(mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

const demoManifest = `{"id":"demo","version":"1.0.0","kind":"provider","run":{"command":["./run.sh"]}}`

func TestInstallZipUnpacksAndKeepsTheExecBit(t *testing.T) {
	r := newRig(t)
	data := zipOf(t, map[string]string{"demo/plugin.json": demoManifest, "demo/run.sh": "#!/bin/sh\n", "demo/lib/x.txt": "x"},
		map[string]os.FileMode{"demo/run.sh": 0o755})
	in, err := r.m.InstallZip(context.Background(), data)
	if err != nil || in.ID != "demo" || in.Name != "demo" {
		t.Fatalf("install = %+v, %v", in, err)
	}
	run := filepath.Join(r.root, "demo", "app", "run.sh")
	if st, err := os.Stat(run); err != nil || st.Mode()&0o100 == 0 {
		t.Errorf("run.sh = %v, %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "demo", "app", "lib", "x.txt")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(r.root, "demo", "data")); err != nil {
		t.Error("the data folder should exist")
	}
}

func TestInstallRefusesUnsafeOrBrokenBundles(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	for name, data := range map[string][]byte{
		"not a zip":       []byte("hello"),
		"no manifest":     zipOf(t, map[string]string{"a.txt": "x"}, nil),
		"traversal":       zipOf(t, map[string]string{"plugin.json": demoManifest, "run.sh": "x", "../evil.txt": "x"}, nil),
		"missing program": zipOf(t, map[string]string{"plugin.json": demoManifest}, nil),
		"bad manifest":    zipOf(t, map[string]string{"plugin.json": `{"id":"X"}`}, nil),
		"symlink":         zipOf(t, map[string]string{"plugin.json": demoManifest, "run.sh": "x", "link": "run.sh"}, map[string]os.FileMode{"link": os.ModeSymlink | 0o777}),
		"two top folders": zipOf(t, map[string]string{"a/plugin.json": demoManifest, "b/run.sh": "x"}, nil),
	} {
		if _, err := r.m.InstallZip(ctx, data); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := os.Stat(filepath.Join(r.root, "evil.txt")); err == nil {
		t.Error("a path traversal escaped the install folder")
	}
	if len(r.m.List(ctx)) != 0 {
		t.Errorf("failed installs left bundles behind: %+v", r.m.List(ctx))
	}
	if entries, _ := os.ReadDir(filepath.Join(r.root, "demo")); len(entries) != 0 {
		t.Errorf("failed install left files: %v", entries)
	}
}

func TestInstallIsRefusedWhenBundlesAreSwitchedOff(t *testing.T) {
	m := NewManager(Options{Root: t.TempDir(), Enabled: false, Store: NewMemStore()})
	if _, err := m.Install(context.Background(), helperBundle("demo", "provider", "")); err != ErrDisabled {
		t.Errorf("err = %v", err)
	}
}

func TestProviderBundleLifecycle(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://secret-password@db/reelstash")
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.m.Install(ctx, helperBundle("helper", "provider", `,{"key":"apiKey","label":"Key","env":"HELPER_API_KEY","secret":true,"required":true}`)); err != nil {
		t.Fatal(err)
	}
	// A required setting is empty: nothing runs yet.
	in := r.waitStatus(t, "helper", StatusNeedsConfig)
	if len(in.Missing) != 1 || in.Missing[0] != "apiKey" {
		t.Errorf("missing = %v", in.Missing)
	}

	if err := r.m.SetConfig(ctx, "helper", map[string]string{"apiKey": "k-123"}); err != nil {
		t.Fatal(err)
	}
	in = r.waitStatus(t, "helper", StatusRunning)
	if in.ProviderID != "x-helper" || in.PID == 0 {
		t.Errorf("info = %+v", in)
	}
	p, ok := r.reg.Get("x-helper")
	if !ok || !p.Enabled() || p.(interface{ Bundle() string }).Bundle() != "helper" || !p.Capabilities().Media {
		t.Fatalf("the provider is not live: %v %v", p, ok)
	}
	if len(p.ConfigFields()) != 0 {
		t.Error("a bundled provider has no token field of its own")
	}
	if err := p.Test(ctx); err != nil {
		t.Errorf("the provider's token should work: %v", err)
	}

	// The service saw its settings but none of ReelStash's own environment.
	raw, _ := os.ReadFile(filepath.Join(r.root, "helper", "data", "env.txt"))
	env := string(raw)
	if !strings.Contains(env, "HELPER_API_KEY=k-123") || !strings.Contains(env, "HOST=127.0.0.1") || !strings.Contains(env, "REELSTASH_PLUGIN_TOKEN=") {
		t.Errorf("service env is missing something:\n%s", env)
	}
	if strings.Contains(env, "DATABASE_URL") || strings.Contains(env, "secret-password") {
		t.Error("the service inherited the server's environment")
	}

	// Switching it off stops the process and takes it out of matching, but
	// the provider stays listed so links keep their name.
	if err := r.m.SetEnabled(ctx, "helper", false); err != nil {
		t.Fatal(err)
	}
	if in, _ := r.m.Get(ctx, "helper"); in.Status != StatusStopped || in.Enabled {
		t.Errorf("after disabling: %+v", in)
	}
	if p, ok := r.reg.Get("x-helper"); !ok || p.Enabled() || len(r.reg.Enabled(provider.KindMedia)) != 0 {
		t.Error("a disabled bundle must stay listed but out of matching")
	}
	if err := r.m.SetEnabled(ctx, "helper", true); err != nil {
		t.Fatal(err)
	}
	r.waitStatus(t, "helper", StatusRunning)
	if p, _ := r.reg.Get("x-helper"); !p.Enabled() {
		t.Error("re-enabling should reconnect it")
	}

	// Uninstalling removes the files, the settings and the provider.
	if _, err := r.m.Remove(ctx, "helper"); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.reg.Get("x-helper"); ok {
		t.Error("the provider is still registered")
	}
	if _, err := os.Stat(filepath.Join(r.root, "helper")); !os.IsNotExist(err) {
		t.Error("the bundle folder is still there")
	}
	if cfg := r.m.Config(ctx, "helper"); len(cfg) != 0 {
		t.Errorf("settings left behind: %v", cfg)
	}
	if _, err := r.m.Get(ctx, "helper"); err != ErrNotFound {
		t.Errorf("get after remove = %v", err)
	}
}

func TestAuthBundleRegistersItsSignInMethod(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.m.Install(ctx, helperBundle("sso", "auth", "")); err != nil {
		t.Fatal(err)
	}
	r.waitStatus(t, "sso", StatusRunning)
	if p, ok := r.auth.Get("sso"); !ok || p.Name() != "Helper Auth" {
		t.Fatalf("auth plugin not registered: %v %v", p, ok)
	}
	if _, ok := r.reg.Get("x-sso"); ok {
		t.Error("an auth bundle must not appear as a metadata provider")
	}
	r.m.SetEnabled(ctx, "sso", false)
	if _, ok := r.auth.Get("sso"); ok {
		t.Error("a disabled auth plugin must leave the sign-in screen")
	}
}

func TestCrashingBundleIsRetriedThenGivenUp(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	if _, err := r.m.Install(ctx, helperBundle("flaky", "provider", "")); err != nil {
		t.Fatal(err)
	}
	r.waitStatus(t, "flaky", StatusRunning)
	r.m.SetConfig(ctx, "flaky", map[string]string{"mode": "crash"})
	in := r.waitStatus(t, "flaky", StatusFailed)
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(in.Error, "gave up") && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		in, _ = r.m.Get(ctx, "flaky")
	}
	if !strings.Contains(in.Error, "gave up") || in.Restarts < maxCrashes-2 {
		t.Errorf("info = %+v", in)
	}
	logs, _ := r.m.Logs("flaky")
	if !strings.Contains(strings.Join(logs, "\n"), "[reelstash]") {
		t.Errorf("the log should say what happened: %v", logs)
	}

	// Fixing the problem and pressing Restart brings it back.
	r.m.SetConfig(ctx, "flaky", map[string]string{"mode": ""})
	r.waitStatus(t, "flaky", StatusRunning)
}

func TestBundleWhoseManifestIdDiffersIsRejected(t *testing.T) {
	r := newRig(t)
	if _, err := r.m.Install(context.Background(), helperBundle("liar", "provider", "")); err != nil {
		t.Fatal(err)
	}
	r.m.SetConfig(context.Background(), "liar", map[string]string{"mode": "wrong-id"})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		in, _ := r.m.Get(context.Background(), "liar")
		if strings.Contains(in.Error, "manifest id") {
			if _, ok := r.reg.Get("x-liar"); ok {
				t.Error("a mismatched provider must not be registered")
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	in, _ := r.m.Get(context.Background(), "liar")
	t.Fatalf("expected a manifest id error, got %+v", in)
}

func TestInstallRefusesAnIdTakenByAConnectedProvider(t *testing.T) {
	r := newRig(t)
	r.reg.Register(extprovider.NewProvider(extprovider.Spec{ID: "x-taken", Name: "Taken", BaseURL: "http://elsewhere", Caps: extprovider.Capabilities{Media: true}}, ""))
	if _, err := r.m.Install(context.Background(), helperBundle("taken", "provider", "")); err == nil || !strings.Contains(err.Error(), "already connected") {
		t.Errorf("err = %v", err)
	}
}

func TestUpgradeKeepsSettingsAndTheOffSwitch(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.m.Install(ctx, helperBundle("up", "provider", ""))
	r.waitStatus(t, "up", StatusRunning)
	r.m.SetConfig(ctx, "up", map[string]string{"mode": "x"})
	r.waitStatus(t, "up", StatusRunning)
	r.m.SetEnabled(ctx, "up", false)

	v2 := helperBundle("up", "provider", "")
	v2[ManifestFile] = &fstest.MapFile{Data: []byte(strings.Replace(string(v2[ManifestFile].Data), `"1.0.0"`, `"2.0.0"`, 1))}
	in, err := r.m.Install(ctx, v2)
	if err != nil || in.Version != "2.0.0" || in.Enabled {
		t.Fatalf("upgrade = %+v, %v", in, err)
	}
	if cfg := r.m.Config(ctx, "up"); cfg["mode"] != "x" {
		t.Errorf("settings lost on upgrade: %v", cfg)
	}
}

func TestLoadFindsInstalledBundlesAfterARestart(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.m.Install(ctx, helperBundle("again", "provider", ""))
	r.waitStatus(t, "again", StatusRunning)
	r.m.SetEnabled(ctx, "again", false)

	again := NewManager(Options{Root: r.root, Enabled: true, Store: r.m.opts.Store, Providers: provider.NewRegistry(), Auth: authplugin.NewRegistry()})
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	list := again.List(ctx)
	if len(list) != 1 || list[0].ID != "again" || list[0].Enabled || list[0].Status != StatusStopped {
		t.Errorf("after restart: %+v", list)
	}
}

func TestProviderStaysListedAfterARestartBeforeItsServiceIsUp(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	r.m.Install(ctx, helperBundle("named", "provider", ""))
	r.waitStatus(t, "named", StatusRunning)
	r.m.SetEnabled(ctx, "named", false)

	// A fresh server start: nothing runs yet, but titles linked to the
	// provider can still show its name, and it is not used for matching.
	reg := provider.NewRegistry()
	again := NewManager(Options{Root: r.root, Enabled: true, Store: r.m.opts.Store, Providers: reg, Auth: authplugin.NewRegistry()})
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	p, ok := reg.Get("x-named")
	if !ok || p.Name() != "Helper" || p.Enabled() || len(reg.Enabled(provider.KindMedia)) != 0 {
		t.Fatalf("after a restart: %v %v", p, ok)
	}
}

// zipOfBytes is a zip of files, without needing a *testing.T.
func zipOfBytes(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}
