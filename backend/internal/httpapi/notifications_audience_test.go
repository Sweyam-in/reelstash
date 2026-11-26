package httpapi

import "testing"

func TestMediaSurfaceSkipsAdminNotices(t *testing.T) {
	if !includeAdminNotices("ADMIN", "") || !includeAdminNotices("ADMIN", "admin") {
		t.Fatal("admin console should include admin notices")
	}
	if includeAdminNotices("ADMIN", "media") || includeAdminNotices("USER", "") || includeAdminNotices("USER", "media") {
		t.Fatal("the media app and regular users should not include admin notices")
	}
}
