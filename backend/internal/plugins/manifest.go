// Package plugins installs and runs plugin bundles: a folder (or .zip) with a
// plugin.json and a script that serves one of ReelStash's HTTP plugin
// contracts. ReelStash starts the script itself, keeps it running, and
// connects it as a metadata provider (docs/provider-plugins.md) or a sign-in
// method (docs/auth-plugins.md). See docs/plugin-bundles.md.
package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ManifestFile is the file every bundle carries at its root.
const ManifestFile = "plugin.json"

// Kind says which contract a bundle's service speaks.
type Kind string

const (
	KindProvider Kind = "provider"
	KindAuth     Kind = "auth"
)

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}$`)
	envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	keyPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,63}$`)
)

// Manifest is a bundle's plugin.json.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Kind        Kind   `json:"kind"`
	Description string `json:"description,omitempty"`
	Homepage    string `json:"homepage,omitempty"`
	// UpdateURL is the address of the index.json that lists this plugin's
	// published versions, so ReelStash can offer updates.
	UpdateURL string        `json:"updateUrl,omitempty"`
	Run       Run           `json:"run"`
	Config    []ConfigField `json:"config,omitempty"`
}

// Run says how to start the service.
type Run struct {
	// Command is the program and its arguments, run from the bundle's folder.
	// The program is either a name found on PATH ("python3", "node") or a file
	// inside the bundle ("./server"). It must listen on 127.0.0.1:$PORT.
	Command []string `json:"command"`
}

// ConfigField is one setting the admin fills in for the bundle. Its value
// reaches the service as the environment variable Env.
type ConfigField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Env         string `json:"env,omitempty"` // defaults to the key in upper case
	Secret      bool   `json:"secret,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
}

// EnvName is the environment variable the field's value is passed in.
func (f ConfigField) EnvName() string {
	if f.Env != "" {
		return f.Env
	}
	return strings.ToUpper(f.Key)
}

// reservedEnv are names a bundle may not set through its settings: they are
// either owned by ReelStash or change how the program is loaded.
func reservedEnv(name string) bool {
	switch name {
	case "PATH", "HOME", "PORT", "HOST", "PWD", "SHELL", "USER":
		return true
	}
	return strings.HasPrefix(name, "REELSTASH_") || strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_")
}

// ParseManifest reads and validates a plugin.json.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("%s is not valid JSON: %w", ManifestFile, err)
	}
	if !idPattern.MatchString(m.ID) {
		return Manifest{}, errors.New("id must be 2-31 lowercase letters, digits or dashes")
	}
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" {
		m.Name = m.ID
	}
	m.Version = strings.TrimSpace(m.Version)
	if m.Version == "" {
		return Manifest{}, errors.New("version is required")
	}
	if m.Kind != KindProvider && m.Kind != KindAuth {
		return Manifest{}, fmt.Errorf("kind must be %q or %q", KindProvider, KindAuth)
	}
	if err := validateCommand(m.Run.Command); err != nil {
		return Manifest{}, err
	}
	seenKey, seenEnv := map[string]bool{}, map[string]bool{}
	for i := range m.Config {
		f := &m.Config[i]
		if !keyPattern.MatchString(f.Key) {
			return Manifest{}, fmt.Errorf("config key %q is not a plain name", f.Key)
		}
		if strings.TrimSpace(f.Label) == "" {
			f.Label = f.Key
		}
		env := f.EnvName()
		if !envPattern.MatchString(env) || reservedEnv(env) {
			return Manifest{}, fmt.Errorf("config %q cannot use the environment variable %q", f.Key, env)
		}
		if seenKey[f.Key] || seenEnv[env] {
			return Manifest{}, fmt.Errorf("config %q is listed twice", f.Key)
		}
		seenKey[f.Key], seenEnv[env] = true, true
	}
	return m, nil
}

func validateCommand(cmd []string) error {
	if len(cmd) == 0 || strings.TrimSpace(cmd[0]) == "" {
		return errors.New("run.command is required")
	}
	for _, a := range cmd {
		if strings.ContainsRune(a, 0) {
			return errors.New("run.command has an invalid character")
		}
	}
	prog := cmd[0]
	if strings.Contains(prog, "/") {
		if !strings.HasPrefix(prog, "./") || strings.Contains(prog, "..") {
			return errors.New(`run.command[0] must be a program name or a file inside the bundle, like "./server"`)
		}
	}
	return nil
}

// MissingRequired lists the required settings that have no value (saved or default).
func (m Manifest) MissingRequired(values map[string]string) []string {
	var out []string
	for _, f := range m.Config {
		if f.Required && strings.TrimSpace(values[f.Key]) == "" && f.Default == "" {
			out = append(out, f.Key)
		}
	}
	return out
}
