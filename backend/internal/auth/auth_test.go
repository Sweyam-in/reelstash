package auth

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/testdb"
)

func TestRandomTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		tok, err := randomToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[tok] {
			t.Fatalf("duplicate token %q", tok)
		}
		seen[tok] = true
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("REELSTASH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("REELSTASH_TEST_DATABASE_URL not set; skipping Postgres integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := testdb.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestUpsertIdentityUserKeepsAdminRole(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	sub := "sso-" + uuid.NewString()
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM app_user WHERE auth_subject = $1`, sub) })

	u, err := UpsertIdentityUser(ctx, pool, Claims{Subject: sub, Email: "a@x.test", DisplayName: "A"}, "USER")
	if err != nil || u.Role != "USER" || u.Email != "a@x.test" || u.Disabled {
		t.Fatalf("first login: %+v %v", u, err)
	}
	// Profile fields refresh on each login.
	u2, _ := UpsertIdentityUser(ctx, pool, Claims{Subject: sub, Email: "b@x.test", DisplayName: "B"}, "USER")
	if u2.ID != u.ID || u2.Email != "b@x.test" || u2.DisplayName != "B" {
		t.Errorf("profile not refreshed: %+v", u2)
	}
	// An admin stays an admin even when a later login resolves to USER.
	pool.Exec(ctx, `UPDATE app_user SET role = 'ADMIN' WHERE id = $1`, u.ID)
	u4, _ := UpsertIdentityUser(ctx, pool, Claims{Subject: sub}, "USER")
	if u4.Role != "ADMIN" {
		t.Errorf("an existing admin must not be demoted by a later login, got %s", u4.Role)
	}
}

func TestSessionLifecycle(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	u, err := UpsertLocalAdmin(ctx, pool)
	if err != nil || u.Role != "ADMIN" || u.Subject != "local-admin" {
		t.Fatalf("local admin: %+v %v", u, err)
	}
	again, _ := UpsertLocalAdmin(ctx, pool)
	if again.ID != u.ID {
		t.Error("the local admin must be a single reused identity")
	}

	id, exp, err := CreateSession(ctx, pool, u.ID, "")
	if err != nil || id == "" {
		t.Fatal(err)
	}
	if d := time.Until(exp); d < SessionTTL-time.Minute || d > SessionTTL+time.Minute {
		t.Errorf("session expiry in %v, want ~%v", d, SessionTTL)
	}
	got, err := LoadSessionUser(ctx, pool, id)
	if err != nil || got.ID != u.ID {
		t.Fatalf("load: %+v %v", got, err)
	}

	if _, err := LoadSessionUser(ctx, pool, "no-such-session"); err != ErrSessionNotFound {
		t.Errorf("unknown session err = %v", err)
	}

	pool.Exec(ctx, `UPDATE web_session SET expires_at = now() - interval '1 second' WHERE id = $1`, id)
	if _, err := LoadSessionUser(ctx, pool, id); err == nil {
		t.Error("expired session must not load")
	}

	id2, _, _ := CreateSession(ctx, pool, u.ID, "")
	if err := DeleteSession(ctx, pool, id2); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSessionUser(ctx, pool, id2); err == nil {
		t.Error("deleted session still loads")
	}
	pool.Exec(ctx, `DELETE FROM web_session WHERE id = $1`, id)
}
