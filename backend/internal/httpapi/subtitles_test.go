package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestSrtToVTT(t *testing.T) {
	in := "1\r\n00:00:01,5 --> 00:00:02,250\r\nHello, world\r\n\r\n2\r\n01:02:03,004 --> 01:02:04,000\r\n<i>Second</i>\r\n"
	got := srtToVTT(in)
	for _, want := range []string{"WEBVTT\n\n", "00:00:01.500 --> 00:00:02.250", "01:02:03.004 --> 01:02:04.000", "Hello, world", "<i>Second</i>"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "\r") || strings.Contains(got, "00:00:01,5") {
		t.Errorf("CRLF or comma timestamps left in\n%s", got)
	}
}

func TestDecodeSubtitleText(t *testing.T) {
	if got := decodeSubtitleText([]byte("\xEF\xBB\xBFcafé")); got != "café" {
		t.Errorf("BOM not stripped: %q", got)
	}
	if got := decodeSubtitleText([]byte("caf\xe9 ol\xe9")); got != "café olé" {
		t.Errorf("windows-1252 not decoded: %q", got)
	}
}

func TestSubtitleRoutesNeedAccessToTheTitle(t *testing.T) {
	h := newHarness(t)
	item := h.newTitle(uniq("Hidden")) // no library: a plain user cannot open it
	h.expect(h.do(&h.user, "GET", "/api/media/"+itoa(item)+"/tracks", nil), http.StatusNotFound)
	h.expect(h.do(&h.user, "GET", "/api/media/"+itoa(item)+"/subtitles/x1.vtt", nil), http.StatusNotFound)
	h.expect(h.do(&h.user, "GET", "/api/media/"+itoa(item)+"/next", nil), http.StatusNotFound)
	h.expect(h.do(nil, "GET", "/api/media/"+itoa(item)+"/tracks", nil), http.StatusUnauthorized)
}
