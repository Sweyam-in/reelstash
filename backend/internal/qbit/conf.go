package qbit

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PasswordHash encodes password the way qBittorrent stores its WebUI
// password: PBKDF2-HMAC-SHA512, 100,000 rounds, a 16-byte salt and a 64-byte
// key, as `@ByteArray(<salt>:<key>)` in base64.
func PasswordHash(password string, salt []byte) (string, error) {
	if salt == nil {
		salt = make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return "", err
		}
	}
	key, err := pbkdf2.Key(sha512.New, password, salt, 100000, 64)
	if err != nil {
		return "", err
	}
	return "@ByteArray(" + base64.StdEncoding.EncodeToString(salt) + ":" + base64.StdEncoding.EncodeToString(key) + ")", nil
}

// SeedConfig writes the WebUI username and password into qBittorrent's
// config file before qBittorrent starts, so the credentials in ReelStash's
// environment are the ones qBittorrent accepts. It also turns off the Host
// header check: the WebUI is reached through a published port (8181 -> 8080)
// or a tunnel hostname, which that check rejects; the password still guards
// it. A missing file is created with just enough to start; an existing one
// keeps everything else.
func SeedConfig(path, username, password string) error {
	if username == "" || password == "" {
		return errors.New("a username and password are required")
	}
	if len(password) < 6 {
		return errors.New("qBittorrent needs a WebUI password of at least 6 characters")
	}
	hash, err := PasswordHash(password, nil)
	if err != nil {
		return err
	}
	set := map[string]string{
		`WebUI\Username`:             username,
		`WebUI\Password_PBKDF2`:      `"` + hash + `"`,
		`WebUI\HostHeaderValidation`: "false",
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(raw)
	if content == "" {
		content = "[LegalNotice]\nAccepted=true\n\n[Preferences]\n"
	}
	out := upsertINI(content, "Preferences", set)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".reelstash-tmp"
	if err := os.WriteFile(tmp, []byte(out), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// upsertINI sets keys inside [section], replacing existing lines and adding
// missing ones (and the section) at its end.
func upsertINI(content, section string, set map[string]string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	header := "[" + section + "]"
	start, end := -1, len(lines)
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == header {
			start = i
			continue
		}
		if start >= 0 && i > start && strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			end = i
			break
		}
	}
	if start < 0 {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, header)
		start, end = len(lines)-1, len(lines)
	}
	done := map[string]bool{}
	for i := start + 1; i < end; i++ {
		key, _, ok := strings.Cut(lines[i], "=")
		if !ok {
			continue
		}
		if v, want := set[strings.TrimSpace(key)]; want {
			lines[i] = strings.TrimSpace(key) + "=" + v
			done[strings.TrimSpace(key)] = true
		}
	}
	var add []string
	for _, k := range SortedKeys(set) {
		if !done[k] {
			add = append(add, k+"="+set[k])
		}
	}
	// Insert before trailing blank lines of the section.
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = append(lines[:at], append(add, lines[at:]...)...)
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}
