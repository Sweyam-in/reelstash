package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"reelstash/backend/internal/auth"
	"reelstash/backend/internal/config"
)

// bearer sends a request with an app's bearer token instead of a cookie.
func (h *harness) bearer(token, method, path, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func TestServerInfoIsPublic(t *testing.T) {
	h := freshRig(t, config.Config{ServerName: "Home"})
	info := h.jsonBody(h.do(nil, "GET", "/auth/server-info", nil), 200)
	if info["product"] != "reelstash" || info["name"] != "Home" || info["apiVersion"] != float64(apiVersion) || info["pairing"] != true {
		t.Errorf("server-info = %v", info)
	}
}

func TestTokenLoginGivesABearerDeviceSession(t *testing.T) {
	h := freshRig(t, config.Config{})
	ctx := context.Background()
	if _, err := auth.CreateNativeUser(ctx, h.pool, "bob", "correct horse", "Bob", "USER"); err != nil {
		t.Fatal(err)
	}
	if code := h.do(nil, "POST", "/auth/token", map[string]string{"username": "bob", "password": "nope"}).Code; code != 401 {
		t.Fatalf("wrong password = %d, want 401", code)
	}
	out := h.jsonBody(h.do(nil, "POST", "/auth/token", map[string]string{
		"username": "bob", "password": "correct horse", "deviceName": "Pixel 9", "platform": "android",
	}), 200)
	token, _ := out["token"].(string)
	if token == "" || out["user"].(map[string]any)["displayName"] != "Bob" {
		t.Fatalf("token login = %v", out)
	}

	me := h.bearer(token, "GET", "/api/me", "")
	if me.Code != 200 {
		t.Fatalf("/api/me with bearer = %d", me.Code)
	}
	if h.bearer("not-a-token", "GET", "/api/me", "").Code != 401 {
		t.Error("an unknown bearer token must be 401")
	}

	devices := h.bearer(token, "GET", "/api/devices", "")
	if devices.Code != 200 || !strings.Contains(devices.Body.String(), `"name":"Pixel 9"`) || !strings.Contains(devices.Body.String(), `"current":true`) {
		t.Fatalf("devices = %d %s", devices.Code, devices.Body.String())
	}
	var id int64
	h.pool.QueryRow(ctx, `SELECT public_id FROM web_session WHERE id = $1`, token).Scan(&id)

	// Another user cannot revoke it; its owner can, and the token stops working.
	var bobID int64
	h.pool.QueryRow(ctx, `SELECT id FROM app_user WHERE username = 'bob'`).Scan(&bobID)
	if ok, _ := auth.RevokeDeviceSession(ctx, h.pool, bobID+100000, id); ok {
		t.Error("revoking another user's device must do nothing")
	}
	if rec := h.bearer(token, "DELETE", "/api/devices/"+strconv.FormatInt(id, 10), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", rec.Code, rec.Body.String())
	}
	if h.bearer(token, "GET", "/api/me", "").Code != 401 {
		t.Error("a revoked device must be signed out")
	}
}

func TestDevicePairing(t *testing.T) {
	h := newHarness(t)
	start := h.jsonBody(h.do(nil, "POST", "/auth/device/start", map[string]string{"deviceName": "Living room TV", "platform": "androidtv"}), 200)
	deviceCode, _ := start["deviceCode"].(string)
	userCode, _ := start["userCode"].(string)
	if deviceCode == "" || len(userCode) != 9 || !strings.HasSuffix(start["verificationUrl"].(string), "/link") {
		t.Fatalf("start = %v", start)
	}
	poll := func() *httptest.ResponseRecorder {
		return h.do(nil, "POST", "/auth/device/poll", map[string]string{"deviceCode": deviceCode})
	}
	if rec := poll(); rec.Code != http.StatusAccepted {
		t.Fatalf("pending poll = %d %s", rec.Code, rec.Body.String())
	}

	// The person types the code loosely: lower case, no dash.
	typed := strings.ToLower(strings.ReplaceAll(userCode, "-", " "))
	look := h.jsonBody(h.do(&h.user, "GET", "/api/devices/pair?code="+strings.ReplaceAll(typed, " ", ""), nil), 200)
	if look["deviceName"] != "Living room TV" {
		t.Errorf("lookup = %v", look)
	}
	if code := h.do(&h.user, "POST", "/api/devices/pair", map[string]string{"code": "ZZZZ-ZZZZ"}).Code; code != 404 {
		t.Errorf("a wrong code = %d, want 404", code)
	}
	h.expect(h.do(&h.user, "POST", "/api/devices/pair", map[string]string{"code": typed}), 200)
	if code := h.do(&h.other, "POST", "/api/devices/pair", map[string]string{"code": userCode}).Code; code != 404 {
		t.Errorf("approving an approved code again = %d, want 404", code)
	}

	out := h.jsonBody(poll(), 200)
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatalf("approved poll = %v", out)
	}
	var owner int64
	h.pool.QueryRow(context.Background(), `SELECT user_id FROM web_session WHERE id = $1`, token).Scan(&owner)
	if owner != h.user.id {
		t.Errorf("session belongs to %d, want the approving user %d", owner, h.user.id)
	}
	if poll().Code != 404 {
		t.Error("a pairing must only be claimed once")
	}
	if h.bearer(token, "GET", "/api/me", "").Code != 200 {
		t.Error("the paired token must work")
	}
}

func TestPairingNeedsASignedInApprover(t *testing.T) {
	h := newHarness(t)
	if code := h.do(nil, "POST", "/api/devices/pair", map[string]string{"code": "BBBB-BBBB"}).Code; code != 401 {
		t.Errorf("anonymous approve = %d, want 401", code)
	}
}

func TestNormalizeUserCode(t *testing.T) {
	for in, want := range map[string]string{"bcdf-ghjk": "BCDF-GHJK", " BCDF GHJK ": "BCDF-GHJK", "bcdfghjk": "BCDF-GHJK", "BCD": "", "BCDFGHJKM": ""} {
		if got := auth.NormalizeUserCode(in); got != want {
			t.Errorf("NormalizeUserCode(%q) = %q, want %q", in, got, want)
		}
	}
}
