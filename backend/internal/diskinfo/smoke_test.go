package diskinfo

import "testing"

func TestStatRoot(t *testing.T) {
	u, err := Stat("/", DefaultTimeout)
	if err != nil || u.Total <= 0 || u.Available < 0 || u.Used < 0 {
		t.Fatalf("Stat(/) = %+v, %v", u, err)
	}
	m, err := MountOf("/", DefaultTimeout)
	if err != nil {
		t.Fatalf("MountOf(/) error: %v", err)
	}
	t.Logf("usage %+v mount %+v", u, m)
}
