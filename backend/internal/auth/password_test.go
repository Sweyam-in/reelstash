package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "pbkdf2-sha256$600000$") || strings.Contains(hash, "correct horse") {
		t.Errorf("hash = %q", hash)
	}
	if !CheckPassword(hash, "correct horse") {
		t.Error("the right password must match")
	}
	for _, wrong := range []string{"", "correct hors", "Correct horse", "correct horse "} {
		if CheckPassword(hash, wrong) {
			t.Errorf("%q must not match", wrong)
		}
	}
	other, _ := HashPassword("correct horse")
	if other == hash {
		t.Error("two hashes of one password must differ (random salt)")
	}
}

func TestCheckPasswordRejectsMalformedHashes(t *testing.T) {
	good, _ := HashPassword("pw-123456")
	parts := strings.Split(good, "$")
	for name, hash := range map[string]string{
		"empty":             "",
		"wrong scheme":      "md5$1$abc$def",
		"too few parts":     "pbkdf2-sha256$600000$abc",
		"bad iterations":    "pbkdf2-sha256$lots$" + parts[2] + "$" + parts[3],
		"zero iterations":   "pbkdf2-sha256$0$" + parts[2] + "$" + parts[3],
		"absurd iterations": "pbkdf2-sha256$999999999$" + parts[2] + "$" + parts[3],
		"bad salt":          "pbkdf2-sha256$600000$***$" + parts[3],
		"empty hash":        "pbkdf2-sha256$600000$" + parts[2] + "$",
	} {
		if CheckPassword(hash, "pw-123456") {
			t.Errorf("%s: a malformed hash must never match", name)
		}
	}
	// Lowering the iteration count makes the stored hash fail to match.
	if CheckPassword("pbkdf2-sha256$1$"+parts[2]+"$"+parts[3], "pw-123456") {
		t.Error("a hash with a tampered iteration count must not match")
	}
}

func TestUsernameAndPasswordRules(t *testing.T) {
	for _, ok := range []string{"abc", "Alice", "a.b-c_d@e", "user123", strings.Repeat("a", 64)} {
		if err := ValidateUsername(ok); err != nil {
			t.Errorf("%q should be fine: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "ab", "a b", "-lead", ".lead", "semi;colon", "naïve", strings.Repeat("a", 65), "a/b"} {
		if err := ValidateUsername(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if ValidatePassword("1234567") == nil || ValidatePassword("12345678") != nil || ValidatePassword(strings.Repeat("x", 257)) == nil {
		t.Error("passwords must be 8-256 characters")
	}
	if ValidatePassword("пароль12") != nil {
		t.Error("length counts characters, not bytes")
	}
}
