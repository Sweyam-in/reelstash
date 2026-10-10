package authplugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func fakeService(t *testing.T, m Manifest) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, v any) { json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, r *http.Request) { write(w, m) })
	mux.HandleFunc("/auth/start", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		write(w, map[string]string{"url": "https://idp.test/authorize?cb=" + url.QueryEscape(b["callbackUrl"]) + "&to=" + url.QueryEscape(b["redirectTo"])})
	})
	mux.HandleFunc("/auth/finish", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Query map[string]string }
		json.NewDecoder(r.Body).Decode(&b)
		switch b.Query["code"] {
		case "good":
			write(w, Result{Identity: &Identity{Subject: "u1", Email: "u@x.test", Role: "root"}, RedirectTo: "/back"})
		case "nosub":
			write(w, Result{Identity: &Identity{}})
		default:
			write(w, Result{Error: "not_authorized"})
		}
	})
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		if b["password"] == "pw" {
			write(w, Result{Identity: &Identity{Subject: b["username"], Role: "ADMIN"}})
			return
		}
		write(w, Result{Error: "login_failed"})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscoverValidatesTheManifest(t *testing.T) {
	ctx := context.Background()
	good := fakeService(t, Manifest{ID: "sso", Name: "SSO", Login: Login{Type: LoginRedirect}})
	p, err := Discover(ctx, good.URL, "tok")
	if err != nil || p.ID() != "sso" || p.Label() != "Continue with SSO" {
		t.Fatalf("discover = %v, %v", p, err)
	}
	if _, err := Discover(ctx, good.URL, "wrong"); err == nil {
		t.Error("a wrong token must be refused")
	}
	for name, m := range map[string]Manifest{
		"bad id":   {ID: "X", Login: Login{Type: LoginRedirect}},
		"no login": {ID: "sso"},
	} {
		srv := fakeService(t, m)
		if _, err := Discover(ctx, srv.URL, "tok"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSubjectKeyNamespacing(t *testing.T) {
	if got := New(Manifest{ID: "ldap"}, "", "").SubjectKey("bob"); got != "ldap:bob" {
		t.Errorf("default = %q", got)
	}
	empty := ""
	if got := New(Manifest{ID: "oidc-auth", SubjectPrefix: &empty}, "", "").SubjectKey("abc"); got != "abc" {
		t.Errorf("explicit empty prefix = %q", got)
	}
}

func TestRedirectFlow(t *testing.T) {
	ctx := context.Background()
	srv := fakeService(t, Manifest{ID: "sso", Name: "SSO", Login: Login{Type: LoginRedirect}})
	p, _ := Discover(ctx, srv.URL, "tok")

	u, err := p.Start(ctx, "http://app/auth/plugin/sso/callback", "/movies")
	if err != nil || u != "https://idp.test/authorize?cb=http%3A%2F%2Fapp%2Fauth%2Fplugin%2Fsso%2Fcallback&to=%2Fmovies" {
		t.Fatalf("start = %q, %v", u, err)
	}
	res, err := p.Finish(ctx, "cb", url.Values{"code": {"good"}})
	if err != nil || res.Identity == nil || res.Identity.Subject != "u1" || res.RedirectTo != "/back" {
		t.Fatalf("finish = %+v, %v", res, err)
	}
	if res.Identity.Role != "USER" {
		t.Errorf("an unknown role must become USER, got %q", res.Identity.Role)
	}
	if res, _ := p.Finish(ctx, "cb", url.Values{"code": {"denied"}}); res.Error != "not_authorized" || res.Identity != nil {
		t.Errorf("denied = %+v", res)
	}
	if _, err := p.Finish(ctx, "cb", url.Values{"code": {"nosub"}}); err == nil {
		t.Error("an identity with no subject must be an error")
	}
	if _, err := p.Password(ctx, "a", "b"); err != ErrUnsupported {
		t.Errorf("password on a redirect plugin = %v", err)
	}
}

func TestPasswordFlow(t *testing.T) {
	ctx := context.Background()
	srv := fakeService(t, Manifest{ID: "ldap", Name: "LDAP", Login: Login{Type: LoginPassword}})
	p, _ := Discover(ctx, srv.URL, "tok")
	if res, err := p.Password(ctx, "bob", "pw"); err != nil || res.Identity.Subject != "bob" || res.Identity.Role != "ADMIN" {
		t.Fatalf("login = %+v, %v", res, err)
	}
	if res, _ := p.Password(ctx, "bob", "no"); res.Error != "login_failed" {
		t.Errorf("wrong password = %+v", res)
	}
	if _, err := p.Start(ctx, "cb", "/"); err != ErrUnsupported {
		t.Errorf("start on a password plugin = %v", err)
	}
}

func TestStartRejectsBadRedirectURLs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"url": "javascript:alert(1)"})
	}))
	defer srv.Close()
	p := New(Manifest{ID: "sso", Login: Login{Type: LoginRedirect}}, srv.URL, "")
	if _, err := p.Start(context.Background(), "cb", "/"); err == nil {
		t.Error("a non-http redirect must be refused")
	}
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(New(Manifest{ID: "b", Name: "Bee"}, "", ""))
	r.Register(New(Manifest{ID: "a", Name: "Aye"}, "", ""))
	if l := r.List(); len(l) != 2 || l[0].ID() != "a" {
		t.Errorf("list = %v", l)
	}
	r.Unregister("a")
	if _, ok := r.Get("a"); ok {
		t.Error("unregistered plugin still found")
	}
	var nilReg *Registry
	if _, ok := nilReg.Get("x"); ok || nilReg.List() != nil {
		t.Error("a nil registry must behave as empty")
	}
}
