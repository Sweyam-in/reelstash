package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reelstash/backend/internal/authplugin"
	"reelstash/backend/internal/extprovider"
	"reelstash/backend/internal/provider"
)

// Status of a bundle's process.
const (
	StatusStopped     = "stopped"      // switched off, or not started yet
	StatusNeedsConfig = "needs-config" // a required setting is empty
	StatusStarting    = "starting"
	StatusRunning     = "running"
	StatusFailed      = "failed" // crashed or never became ready; see Error and the log
)

const (
	readyTimeout  = 30 * time.Second
	stopGrace     = 5 * time.Second
	maxCrashes    = 6
	stableAfter   = 60 * time.Second
	logLines      = 500
	stateKey      = "plugin_bundles"
	configKeyBase = "plugin_config_"
)

// ErrDisabled is returned when running bundles is switched off for the server.
var ErrDisabled = errors.New("plugin bundles are switched off on this server (REELSTASH_PLUGIN_BUNDLES=false)")

// ErrNotFound is returned for a bundle that is not installed.
var ErrNotFound = errors.New("no such plugin bundle")

// Options configure a Manager.
type Options struct {
	// Root is the folder bundles are installed under.
	Root string
	// Enabled is false when installing and running bundles is switched off.
	Enabled   bool
	Store     Store
	Providers *provider.Registry
	Auth      *authplugin.Registry
}

// Manager installs bundles and keeps their processes running.
type Manager struct {
	opts Options

	mu      sync.Mutex
	bundles map[string]*bundle
	ctx     context.Context // set by StartAll; supervisors stop when it ends
	started bool

	// commandHook rewrites a bundle's command before it is run (tests only).
	commandHook func(argv []string) []string
	// retryDelay is how long to wait before restarting after the nth crash.
	retryDelay func(crashes int) time.Duration
}

type bundle struct {
	man     Manifest
	appDir  string
	dataDir string
	enabled bool
	logs    *lineRing

	mu        sync.Mutex
	status    string
	lastErr   string
	pid       int
	restarts  int
	startedAt time.Time
	spec      *extprovider.Spec // the provider's last known description
	cancel    context.CancelFunc
	done      chan struct{}
}

func NewManager(o Options) *Manager {
	return &Manager{opts: o, bundles: map[string]*bundle{}}
}

// Enabled reports whether bundles may be installed and run on this server.
func (m *Manager) Enabled() bool { return m.opts.Enabled }

// Root is the folder bundles live in.
func (m *Manager) Root() string { return m.opts.Root }

type bundleState struct {
	Enabled bool `json:"enabled"`
	// Spec is the provider the bundle last registered, so its name still shows
	// (switched off) before the service has started, or while it is off.
	Spec *extprovider.Spec `json:"spec,omitempty"`
	// UpdateURL overrides the update source in plugin.json.
	UpdateURL string `json:"updateUrl,omitempty"`
}

func (m *Manager) loadStates(ctx context.Context) map[string]bundleState {
	states := map[string]bundleState{}
	if raw, ok := m.opts.Store.Get(ctx, stateKey); ok {
		_ = json.Unmarshal([]byte(raw), &states)
	}
	return states
}

func (m *Manager) saveStates(ctx context.Context, states map[string]bundleState) error {
	raw, _ := json.Marshal(states)
	return m.opts.Store.Put(ctx, stateKey, string(raw))
}

func (m *Manager) setState(ctx context.Context, id string, st *bundleState) error {
	states := m.loadStates(ctx)
	if st == nil {
		delete(states, id)
	} else {
		next := *st
		if next.Spec == nil {
			next.Spec = states[id].Spec
		}
		if next.UpdateURL == "" {
			next.UpdateURL = states[id].UpdateURL
		}
		states[id] = next
	}
	return m.saveStates(ctx, states)
}

// Load finds the bundles already installed on disk. It starts nothing.
func (m *Manager) Load(ctx context.Context) error {
	entries, err := os.ReadDir(m.opts.Root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	states := m.loadStates(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		appDir := filepath.Join(m.opts.Root, e.Name(), "app")
		raw, err := os.ReadFile(filepath.Join(appDir, ManifestFile))
		if err != nil {
			continue
		}
		man, err := ParseManifest(raw)
		if err != nil || man.ID != e.Name() {
			log.Printf("plugin bundle %q skipped: %v", e.Name(), err)
			continue
		}
		st, known := states[man.ID]
		if !known {
			st.Enabled = true
		}
		b := newBundle(man, filepath.Join(m.opts.Root, man.ID), st.Enabled)
		if man.Kind == KindProvider && st.Spec != nil {
			spec := *st.Spec
			b.spec = &spec
			m.detach(b, false) // lists it, switched off, until the service is up
		}
		m.bundles[man.ID] = b
	}
	return nil
}

func newBundle(man Manifest, dir string, enabled bool) *bundle {
	return &bundle{man: man, appDir: filepath.Join(dir, "app"), dataDir: filepath.Join(dir, "data"), enabled: enabled, logs: newLineRing(logLines), status: StatusStopped}
}

// StartAll starts every enabled bundle in the background and ties their
// lifetime to ctx.
func (m *Manager) StartAll(ctx context.Context) {
	m.mu.Lock()
	m.ctx, m.started = ctx, true
	var todo []*bundle
	for _, b := range m.bundles {
		if b.enabled {
			todo = append(todo, b)
		}
	}
	m.mu.Unlock()
	if !m.opts.Enabled {
		return
	}
	for _, b := range todo {
		m.start(b)
	}
}

// Shutdown stops every bundle and waits for the processes to end.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	all := make([]*bundle, 0, len(m.bundles))
	for _, b := range m.bundles {
		all = append(all, b)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, b := range all {
		wg.Add(1)
		go func(b *bundle) { defer wg.Done(); m.stop(b) }(b)
	}
	wg.Wait()
}

// --- queries ---

// Info describes one installed bundle.
type Info struct {
	Manifest
	Enabled    bool       `json:"enabled"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	PID        int        `json:"pid,omitempty"`
	Restarts   int        `json:"restarts"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	Missing    []string   `json:"missing,omitempty"` // required settings with no value
	ProviderID string     `json:"providerId,omitempty"`
	// Previous is the version an install replaced; set only on the result of an upgrade.
	Previous string `json:"previousVersion,omitempty"`
}

func (m *Manager) info(ctx context.Context, b *bundle) Info {
	b.mu.Lock()
	defer b.mu.Unlock()
	in := Info{Manifest: b.man, Enabled: b.enabled, Status: b.status, Error: b.lastErr, PID: b.pid, Restarts: b.restarts}
	if !b.startedAt.IsZero() && b.status == StatusRunning {
		t := b.startedAt
		in.StartedAt = &t
	}
	in.Missing = b.man.MissingRequired(m.Config(ctx, b.man.ID))
	if b.man.Kind == KindProvider {
		in.ProviderID = extprovider.IDPrefix + b.man.ID
	}
	return in
}

// List returns every installed bundle, ordered by name.
func (m *Manager) List(ctx context.Context) []Info {
	m.mu.Lock()
	all := make([]*bundle, 0, len(m.bundles))
	for _, b := range m.bundles {
		all = append(all, b)
	}
	m.mu.Unlock()
	out := make([]Info, 0, len(all))
	for _, b := range all {
		out = append(out, m.info(ctx, b))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (m *Manager) get(id string) (*bundle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.bundles[id]
	if !ok {
		return nil, ErrNotFound
	}
	return b, nil
}

// Get describes one bundle.
func (m *Manager) Get(ctx context.Context, id string) (Info, error) {
	b, err := m.get(id)
	if err != nil {
		return Info{}, err
	}
	return m.info(ctx, b), nil
}

// Logs returns the latest lines the bundle printed, oldest first.
func (m *Manager) Logs(id string) ([]string, error) {
	b, err := m.get(id)
	if err != nil {
		return nil, err
	}
	return b.logs.lines(), nil
}

// --- settings ---

// Config returns a bundle's saved settings (without defaults filled in).
func (m *Manager) Config(ctx context.Context, id string) map[string]string {
	cfg := map[string]string{}
	if raw, ok := m.opts.Store.Get(ctx, configKeyBase+id); ok {
		_ = json.Unmarshal([]byte(raw), &cfg)
	}
	return cfg
}

// SetConfig merges values into a bundle's settings (an empty value clears
// one) and restarts the bundle so the service sees them.
func (m *Manager) SetConfig(ctx context.Context, id string, values map[string]string) error {
	b, err := m.get(id)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range b.man.Config {
		known[f.Key] = true
	}
	merged := m.Config(ctx, id)
	for k, v := range values {
		if !known[k] {
			return fmt.Errorf("unknown setting %q", k)
		}
		if v = strings.TrimSpace(v); v == "" {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	raw, _ := json.Marshal(merged)
	if err := m.opts.Store.Put(ctx, configKeyBase+id, string(raw)); err != nil {
		return err
	}
	return m.Restart(ctx, id)
}

func (m *Manager) effectiveConfig(b *bundle) map[string]string {
	saved := m.Config(context.Background(), b.man.ID)
	out := map[string]string{}
	for _, f := range b.man.Config {
		v := saved[f.Key]
		if v == "" {
			v = f.Default
		}
		out[f.Key] = v
	}
	return out
}

// --- control ---

// SetEnabled switches a bundle on or off; off stops its process.
func (m *Manager) SetEnabled(ctx context.Context, id string, enabled bool) error {
	b, err := m.get(id)
	if err != nil {
		return err
	}
	if enabled && !m.opts.Enabled {
		return ErrDisabled
	}
	if err := m.setState(ctx, id, &bundleState{Enabled: enabled}); err != nil {
		return err
	}
	b.mu.Lock()
	b.enabled = enabled
	b.mu.Unlock()
	m.stop(b)
	if enabled {
		m.start(b)
	}
	return nil
}

// Restart stops and starts a bundle again, also clearing a "failed" state.
func (m *Manager) Restart(ctx context.Context, id string) error {
	b, err := m.get(id)
	if err != nil {
		return err
	}
	m.stop(b)
	b.mu.Lock()
	enabled := b.enabled
	b.mu.Unlock()
	if enabled && m.opts.Enabled {
		m.start(b)
	}
	return nil
}

// Remove stops a bundle and deletes it, its data and its settings.
func (m *Manager) Remove(ctx context.Context, id string) (Info, error) {
	b, err := m.get(id)
	if err != nil {
		return Info{}, err
	}
	info := m.info(ctx, b)
	m.stop(b)
	m.detach(b, true)
	m.mu.Lock()
	delete(m.bundles, id)
	m.mu.Unlock()
	if err := os.RemoveAll(filepath.Join(m.opts.Root, id)); err != nil {
		return info, err
	}
	_ = m.setState(ctx, id, nil)
	_ = m.opts.Store.Delete(ctx, configKeyBase+id)
	return info, nil
}

// --- install ---

// InstallZip installs (or upgrades) a bundle from a .zip held in memory.
func (m *Manager) InstallZip(ctx context.Context, data []byte) (Info, error) {
	fsys, err := openZip(data)
	if err != nil {
		return Info{}, err
	}
	return m.Install(ctx, fsys)
}

// Install installs a bundle from fsys. An installed bundle with the same id is
// upgraded in place: its settings, data and on/off choice are kept.
func (m *Manager) Install(ctx context.Context, fsys fs.FS) (Info, error) {
	if !m.opts.Enabled {
		return Info{}, ErrDisabled
	}
	root, man, err := bundleRoot(fsys)
	if err != nil {
		return Info{}, err
	}
	if man.Kind == KindProvider && m.opts.Providers != nil {
		if p, ok := m.opts.Providers.Get(extprovider.IDPrefix + man.ID); ok {
			if ext, isExt := p.(interface{ Bundle() string }); !isExt || ext.Bundle() != man.ID {
				return Info{}, fmt.Errorf("a provider %q is already connected; remove it first", p.ID())
			}
		}
	}
	dir := filepath.Join(m.opts.Root, man.ID)
	next := filepath.Join(dir, "app.new")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Info{}, err
	}
	_ = os.RemoveAll(next)
	if err := extract(root, next); err != nil {
		_ = os.RemoveAll(next)
		return Info{}, err
	}
	if prog := man.Run.Command[0]; strings.HasPrefix(prog, "./") {
		if st, err := os.Stat(filepath.Join(next, filepath.FromSlash(prog))); err != nil || st.IsDir() {
			_ = os.RemoveAll(next)
			return Info{}, fmt.Errorf("run.command starts %s, which is not in the bundle", prog)
		}
	}

	m.mu.Lock()
	old := m.bundles[man.ID]
	m.mu.Unlock()
	enabled := true
	previous := ""
	if old != nil {
		old.mu.Lock()
		enabled = old.enabled
		previous = old.man.Version
		old.mu.Unlock()
		m.stop(old)
	}
	app := filepath.Join(dir, "app")
	prev := filepath.Join(dir, "app.old")
	_ = os.RemoveAll(prev)
	if _, err := os.Stat(app); err == nil {
		if err := os.Rename(app, prev); err != nil {
			return Info{}, err
		}
	}
	if err := os.Rename(next, app); err != nil {
		_ = os.Rename(prev, app)
		return Info{}, err
	}
	_ = os.RemoveAll(prev)
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0o755); err != nil {
		return Info{}, err
	}
	if err := m.setState(ctx, man.ID, &bundleState{Enabled: enabled}); err != nil {
		return Info{}, err
	}
	b := newBundle(man, dir, enabled)
	m.mu.Lock()
	if old != nil {
		b.logs = old.logs
		b.spec = old.spec
	}
	m.bundles[man.ID] = b
	m.mu.Unlock()
	if enabled {
		m.start(b)
	}
	in := m.info(ctx, b)
	in.Previous = previous
	return in, nil
}

// --- process supervision ---

func (b *bundle) setStatus(status, errText string) {
	b.mu.Lock()
	b.status, b.lastErr = status, errText
	if status != StatusRunning {
		b.pid = 0
	}
	b.mu.Unlock()
}

// start launches the supervisor for b if it is not already running.
func (m *Manager) start(b *bundle) {
	m.mu.Lock()
	parent := m.ctx
	started := m.started
	m.mu.Unlock()
	if !started || parent == nil {
		return // StartAll has not run yet; it will start enabled bundles
	}
	b.mu.Lock()
	if b.cancel != nil {
		b.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	b.cancel, b.done = cancel, done
	b.restarts = 0
	b.lastErr = ""
	b.status = StatusStarting // visible at once, before the supervisor has run
	b.mu.Unlock()
	go func() {
		defer close(done)
		m.supervise(ctx, b)
	}()
}

// stop ends b's process (if any) and waits for it, then takes it out of
// service: a provider stays listed but off, an auth plugin disappears from
// the sign-in screen.
func (m *Manager) stop(b *bundle) {
	b.mu.Lock()
	cancel, done := b.cancel, b.done
	b.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
		b.mu.Lock()
		b.cancel, b.done = nil, nil
		b.mu.Unlock()
	}
	b.setStatus(StatusStopped, "")
	m.detach(b, false)
}

func (m *Manager) supervise(ctx context.Context, b *bundle) {
	crashes := 0
	for {
		ranFor, err := m.runOnce(ctx, b)
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, errNeedsConfig) {
			b.setStatus(StatusNeedsConfig, "")
			return
		}
		if ranFor >= stableAfter {
			crashes = 0
		}
		crashes++
		msg := "stopped unexpectedly"
		if err != nil {
			msg = err.Error()
		}
		b.logs.add("[reelstash] " + msg)
		if crashes >= maxCrashes {
			b.setStatus(StatusFailed, fmt.Sprintf("%s (gave up after %d attempts; fix the problem and press Restart)", msg, crashes))
			m.detach(b, false)
			return
		}
		b.setStatus(StatusFailed, msg+" (restarting)")
		wait := defaultRetryDelay(crashes)
		if m.retryDelay != nil {
			wait = m.retryDelay(crashes)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		b.mu.Lock()
		b.restarts++
		b.mu.Unlock()
	}
}

func defaultRetryDelay(crashes int) time.Duration {
	wait := time.Duration(1<<crashes) * time.Second
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	return wait
}

var errNeedsConfig = errors.New("a required setting is missing")

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomToken() string {
	var buf [24]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// childEnv is everything the service sees. It does not inherit ReelStash's own
// environment, which holds the database password and other secrets.
func (m *Manager) childEnv(b *bundle, port int, token string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + b.dataDir,
		"HOST=127.0.0.1",
		"PORT=" + strconv.Itoa(port),
		"REELSTASH_PLUGIN_ID=" + b.man.ID,
		"REELSTASH_PLUGIN_KIND=" + string(b.man.Kind),
		"REELSTASH_PLUGIN_TOKEN=" + token,
		"REELSTASH_PLUGIN_DATA=" + b.dataDir,
	}
	for _, k := range []string{"LANG", "LC_ALL", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	cfg := m.effectiveConfig(b)
	for _, f := range b.man.Config {
		if v := cfg[f.Key]; v != "" {
			env = append(env, f.EnvName()+"="+v)
		}
	}
	return env
}

// runOnce starts the service, waits until it answers, connects it, and blocks
// until it exits or ctx ends. It returns how long the process ran.
func (m *Manager) runOnce(ctx context.Context, b *bundle) (time.Duration, error) {
	if missing := b.man.MissingRequired(m.Config(ctx, b.man.ID)); len(missing) > 0 {
		return 0, errNeedsConfig
	}
	port, err := freePort()
	if err != nil {
		return 0, err
	}
	token := randomToken()
	argv := append([]string(nil), b.man.Run.Command...)
	if m.commandHook != nil {
		argv = m.commandHook(argv)
	}
	prog := argv[0]
	if strings.HasPrefix(prog, "./") {
		prog = filepath.Join(b.appDir, filepath.FromSlash(prog))
	}
	if err := os.MkdirAll(b.dataDir, 0o755); err != nil {
		return 0, err
	}
	cmd := exec.Command(prog, argv[1:]...)
	cmd.Dir = b.appDir
	cmd.Env = m.childEnv(b, port, token)
	setProcAttr(cmd)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	b.setStatus(StatusStarting, "")
	b.logs.add(fmt.Sprintf("[reelstash] starting %s on port %d", strings.Join(b.man.Run.Command, " "), port))
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("could not start %q: %w", b.man.Run.Command[0], err)
	}
	started := time.Now()
	var pipes sync.WaitGroup
	for _, r := range []interface{ Read([]byte) (int, error) }{stdout, stderr} {
		pipes.Add(1)
		go func(r interface{ Read([]byte) (int, error) }) { defer pipes.Done(); b.logs.scan(r) }(r)
	}
	// exited closes once the process is gone; waitErr is then its exit status.
	exited := make(chan struct{})
	var waitErr error
	go func() {
		pipes.Wait()
		waitErr = cmd.Wait()
		close(exited)
	}()
	b.mu.Lock()
	b.pid = cmd.Process.Pid
	b.mu.Unlock()

	shutdown := func() {
		terminate(cmd)
		select {
		case <-exited:
		case <-time.After(stopGrace):
			kill(cmd)
			<-exited
		}
	}

	baseURL := "http://127.0.0.1:" + strconv.Itoa(port)
	if err := m.waitReady(ctx, baseURL, token, exited, &waitErr); err != nil {
		select {
		case <-exited: // already gone
		default:
			shutdown()
		}
		if ctx.Err() != nil {
			return time.Since(started), ctx.Err()
		}
		return time.Since(started), err
	}
	if err := m.attach(ctx, b, baseURL, token); err != nil {
		shutdown()
		return time.Since(started), err
	}
	b.mu.Lock()
	b.status, b.lastErr, b.startedAt = StatusRunning, "", time.Now()
	b.mu.Unlock()
	log.Printf("plugin bundle %q running (%s, port %d)", b.man.ID, b.man.Kind, port)

	select {
	case <-exited:
		m.detach(b, false)
		if waitErr != nil {
			return time.Since(started), fmt.Errorf("exited: %v", waitErr)
		}
		return time.Since(started), errors.New("exited")
	case <-ctx.Done():
		shutdown()
		return time.Since(started), ctx.Err()
	}
}

// waitReady polls GET /manifest until the service answers, exits, or time runs out.
func (m *Manager) waitReady(ctx context.Context, baseURL, token string, exited <-chan struct{}, waitErr *error) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.NewTimer(readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-exited:
			if *waitErr != nil {
				return fmt.Errorf("exited before it was ready: %v", *waitErr)
			}
			return errors.New("exited before it was ready")
		case <-deadline.C:
			return fmt.Errorf("did not answer on its port within %s", readyTimeout)
		case <-tick.C:
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/manifest", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
	}
}

// attach connects a running service to the registry that uses it.
func (m *Manager) attach(ctx context.Context, b *bundle, baseURL, token string) error {
	switch b.man.Kind {
	case KindProvider:
		spec, err := extprovider.Discover(ctx, baseURL, token)
		if err != nil {
			return fmt.Errorf("its manifest is not usable: %w", err)
		}
		if spec.ID != extprovider.IDPrefix+b.man.ID {
			return fmt.Errorf("its manifest id is %q but the bundle id is %q", strings.TrimPrefix(spec.ID, extprovider.IDPrefix), b.man.ID)
		}
		spec.Bundle = b.man.ID
		b.mu.Lock()
		b.spec = &spec
		b.mu.Unlock()
		states := m.loadStates(ctx)
		st := states[b.man.ID]
		st.Spec = &spec
		states[b.man.ID] = st
		_ = m.saveStates(ctx, states)
		if m.opts.Providers != nil {
			m.opts.Providers.Register(extprovider.NewProvider(spec, token))
		}
	case KindAuth:
		p, err := authplugin.Discover(ctx, baseURL, token)
		if err != nil {
			return fmt.Errorf("its manifest is not usable: %w", err)
		}
		if p.ID() != b.man.ID {
			return fmt.Errorf("its manifest id is %q but the bundle id is %q", p.ID(), b.man.ID)
		}
		if m.opts.Auth != nil {
			m.opts.Auth.Register(p)
		}
	}
	return nil
}

// detach takes a bundle's service out of use. A provider stays registered but
// switched off, so titles it matched still show its name; remove drops it.
func (m *Manager) detach(b *bundle, remove bool) {
	switch b.man.Kind {
	case KindAuth:
		if m.opts.Auth != nil {
			m.opts.Auth.Unregister(b.man.ID)
		}
	case KindProvider:
		if m.opts.Providers == nil {
			return
		}
		id := extprovider.IDPrefix + b.man.ID
		if remove {
			m.opts.Providers.Unregister(id)
			extprovider.ClearCalls(id)
			return
		}
		b.mu.Lock()
		spec := b.spec
		b.mu.Unlock()
		if spec == nil {
			return
		}
		off := *spec
		off.BaseURL, off.Disabled = "", true
		m.opts.Providers.Register(extprovider.NewProvider(off, ""))
	}
}
