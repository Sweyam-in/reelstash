// Package authtest is a minimal in-process OpenID Connect provider for tests:
// discovery, JWKS, and a token endpoint that verifies PKCE and returns an
// RS256-signed ID token. It is never linked into the server binary.
package authtest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type grant struct {
	claims    map[string]any
	challenge string
	redirect  string
}

type IdP struct {
	Server   *httptest.Server
	ClientID string

	// SignWith, when set, signs ID tokens with another provider's key, to
	// play a provider whose tokens do not verify against our JWKS.
	SignWith *IdP

	key    *rsa.PrivateKey
	mu     sync.Mutex
	grants map[string]grant
	n      int
	// TokenAuth records how the last token request authenticated the client.
	tokenAuth TokenAuth
	agents    []string
}

// UserAgents returns the User-Agent of every request the provider has seen.
func (p *IdP) UserAgents() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.agents...)
}

// TokenAuth is how a client authenticated at the token endpoint.
type TokenAuth struct {
	BasicUser, BasicPassword string // client_secret_basic
	FormClientID, FormSecret string // client_secret_post
}

// LastTokenAuth returns how the most recent token request authenticated.
func (p *IdP) LastTokenAuth() TokenAuth {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokenAuth
}

func New(t *testing.T, clientID string) *IdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &IdP{ClientID: clientID, key: key, grants: map[string]grant{}}
	mux := http.NewServeMux()
	record := func(r *http.Request) {
		p.mu.Lock()
		p.agents = append(p.agents, r.UserAgent())
		p.mu.Unlock()
	}
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                p.Server.URL,
			"authorization_endpoint":                p.Server.URL + "/authorize",
			"token_endpoint":                        p.Server.URL + "/token",
			"jwks_uri":                              p.Server.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		record(r)
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test-key",
			"n": b64(key.N.Bytes()),
			"e": b64(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", p.token)
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Server.Close)
	return p
}

// Issuer is the URL to configure as the plugin's issuer setting.
func (p *IdP) Issuer() string { return p.Server.URL }

// IssueCode registers an authorization code that /token will redeem for an ID
// token carrying claims, provided the caller presents the verifier for
// challenge (S256) and the same redirect URI.
func (p *IdP) IssueCode(claims map[string]any, challenge, redirectURI string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.n++
	code := "code-" + string(rune('a'+p.n%26)) + time.Now().Format("150405.000000")
	p.grants[code] = grant{claims: claims, challenge: challenge, redirect: redirectURI}
	return code
}

func (p *IdP) token(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.agents = append(p.agents, r.UserAgent())
	p.mu.Unlock()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", 400)
		return
	}
	p.mu.Lock()
	user, pass, _ := r.BasicAuth()
	p.tokenAuth = TokenAuth{BasicUser: user, BasicPassword: pass, FormClientID: r.Form.Get("client_id"), FormSecret: r.Form.Get("client_secret")}
	g, ok := p.grants[r.Form.Get("code")]
	delete(p.grants, r.Form.Get("code")) // codes are single use
	p.mu.Unlock()
	fail := func(msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": msg})
	}
	if !ok {
		fail("unknown code")
		return
	}
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if b64(sum[:]) != g.challenge {
		fail("pkce verification failed")
		return
	}
	if g.redirect != "" && r.Form.Get("redirect_uri") != g.redirect {
		fail("redirect_uri mismatch")
		return
	}
	claims := map[string]any{
		"iss": p.Server.URL, "aud": p.ClientID,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range g.claims {
		claims[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
		"id_token": p.signer().Sign(claims),
	})
}

func (p *IdP) signer() *IdP {
	if p.SignWith != nil {
		return p.SignWith
	}
	return p
}

// Sign returns an RS256 JWT over claims using the provider's key.
func (p *IdP) Sign(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test-key"})
	body, _ := json.Marshal(claims)
	signing := b64(header) + "." + b64(body)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	if err != nil {
		panic(err)
	}
	return signing + "." + b64(sig)
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
