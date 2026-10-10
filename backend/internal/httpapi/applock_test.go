package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestPinHashRoundTrip(t *testing.T) {
	h, err := hashPin("123456")
	if err != nil {
		t.Fatal(err)
	}
	if !checkPin(h, "123456") || checkPin(h, "123457") || checkPin("garbage", "123456") {
		t.Fatal("pin verification mismatch")
	}
	if validPin("12345") || validPin("12345a") || !validPin("000000") {
		t.Fatal("validPin mismatch")
	}
}

func TestUnlockCookieBoundToUserAndPin(t *testing.T) {
	a := &API{}
	rec := httptest.NewRecorder()
	a.setUnlockCookie(rec, httptest.NewRequest("GET", "/", nil), 7, "hashA", time.Minute)
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(rec.Result().Cookies()[0])
	if unlockRemaining(req, 7, "hashA") == 0 {
		t.Fatal("valid cookie rejected")
	}
	if unlockRemaining(req, 8, "hashA") != 0 || unlockRemaining(req, 7, "hashB") != 0 {
		t.Fatal("cookie accepted for wrong user or changed PIN")
	}
}
