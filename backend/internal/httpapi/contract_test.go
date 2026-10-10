package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The web apps talk to this server through app/src/api.js and admin/src/api.js. This test reads that
// file and checks every endpoint it calls (method + path) is registered on the
// router, so a renamed or removed route fails here instead of as a 404 in the
// browser. It only checks routes exist; behaviour is covered elsewhere.

var (
	helperCall  = regexp.MustCompile("\\b(get|post|put|del)\\(\\s*[`'\"]")
	requestCall = regexp.MustCompile("\\brequest\\(\\s*'([A-Z]+)'\\s*,\\s*[`'\"]")
	fetchCall   = regexp.MustCompile("fetch\\(\\s*`([^`]*)`\\s*,\\s*\\{[^}]*method:\\s*'([A-Z]+)'")
	xhrCall     = regexp.MustCompile(`xhr\.open\('([A-Z]+)',\s*'([^']+)'`)
)

var helperVerb = map[string]string{"get": "GET", "post": "POST", "put": "PUT", "del": "DELETE"}

// literalPath reads a JS string/template literal starting at src[0] (just after
// its opening quote) and returns its path part: `${...}` becomes "1" and the
// query string is dropped.
func literalPath(src string) string {
	var b strings.Builder
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '$' && i+1 < len(src) && src[i+1] == '{':
			depth := 0
			for i++; i < len(src); i++ {
				if src[i] == '{' {
					depth++
				} else if src[i] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
			}
			b.WriteString("1")
		case c == '?' || c == '`' || c == '\'' || c == '"':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

type endpoint struct{ method, path string }

func uiEndpoints(t *testing.T) []endpoint {
	t.Helper()
	// Both web apps keep their own API client; each must only call routes the server has.
	var src strings.Builder
	for _, app := range []string{"app", "admin"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", app, "src", "api.js"))
		if err != nil {
			t.Skipf("%s/src/api.js not available: %v", app, err)
		}
		src.Write(raw)
		src.WriteString("\n")
	}
	srcText := src.String()
	seen := map[endpoint]bool{}
	add := func(method, path string) {
		if strings.HasPrefix(path, "/") {
			seen[endpoint{method, path}] = true
		}
	}
	for _, m := range helperCall.FindAllStringSubmatchIndex(srcText, -1) {
		add(helperVerb[srcText[m[2]:m[3]]], literalPath(srcText[m[1]:]))
	}
	for _, m := range requestCall.FindAllStringSubmatchIndex(srcText, -1) {
		add(srcText[m[2]:m[3]], literalPath(srcText[m[1]:]))
	}
	for _, m := range fetchCall.FindAllStringSubmatch(srcText, -1) {
		add(m[2], literalPath(m[1]+"`"))
	}
	for _, m := range xhrCall.FindAllStringSubmatch(srcText, -1) {
		add(m[1], m[2])
	}
	out := make([]endpoint, 0, len(seen))
	for e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path+out[i].method < out[j].path+out[j].method })
	return out
}

// routeMatcher returns whether some registered route accepts method+path.
func routeMatcher(t *testing.T, h http.Handler) func(method, path string) bool {
	t.Helper()
	mux, ok := h.(*chi.Mux)
	if !ok {
		t.Fatalf("router is %T", h)
	}
	return func(method, path string) bool {
		return mux.Match(chi.NewRouteContext(), method, path)
	}
}

func TestUIEndpointsExistOnTheServer(t *testing.T) {
	h := newHarness(t)
	matches := routeMatcher(t, h.handler)
	endpoints := uiEndpoints(t)
	if len(endpoints) < 150 {
		t.Fatalf("only %d endpoints found in api.js; the parser is broken", len(endpoints))
	}
	for _, e := range endpoints {
		if !matches(e.method, e.path) {
			t.Errorf("ui calls %s %s but the server has no such route", e.method, e.path)
		}
	}
}

func TestContractParserReadsTheShapesApiJsUses(t *testing.T) {
	cases := map[string]string{
		"`/api/media/${id}/markers`":                                    "/api/media/1/markers",
		"'/api/admin/overview'":                                         "/api/admin/overview",
		"`/api/media?${new URLSearchParams(params)}`":                   "/api/media",
		"`/api/x/${a ? `y` : 'z'}/end?q=1`":                             "/api/x/1/end",
		"`/api/series/${id}/providers/${encodeURIComponent(provider)}`": "/api/series/1/providers/1",
	}
	for lit, want := range cases {
		if got := literalPath(lit[1:]); got != want {
			t.Errorf("literalPath(%s) = %q, want %q", lit, got, want)
		}
	}
}

func TestRouteMatcherRejectsUnknownRoutesAndWrongMethods(t *testing.T) {
	h := newHarness(t)
	matches := routeMatcher(t, h.handler)
	if !matches("GET", "/api/media/7/markers") || !matches("DELETE", "/api/admin/jobs/finished") {
		t.Fatal("known routes must match")
	}
	for _, c := range []endpoint{
		{"GET", "/api/nope"},
		{"DELETE", "/api/media/7/markers"}, // route exists, but not for DELETE
		{"POST", "/api/me"},
	} {
		if matches(c.method, c.path) {
			t.Errorf("%s %s should not match", c.method, c.path)
		}
	}
}
