package events

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"reelstash/backend/internal/testdb"
)

// Two hubs bridged over the same database stand in for the media and admin
// servers: a relayed event reaches the other hub's subscribers, events that
// are not relayed stay local, and nothing echoes back to the sender.
func TestBridgeRelaysNamedEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	pool, err := pgxpool.New(ctx, testdb.URL(t))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	// Stop the listeners (which hold connections) before closing the pool.
	defer func() { cancel(); pool.Close() }()

	admin, media := New(), New()
	Bridge(ctx, pool, admin, "notification")
	Bridge(ctx, pool, media, "notification")
	time.Sleep(300 * time.Millisecond) // let both listeners start

	mediaSub, stopMedia := media.Subscribe(false)
	defer stopMedia()
	adminSub, stopAdmin := admin.Subscribe(true)
	defer stopAdmin()

	admin.Publish("notification", map[string]string{"title": "New titles"}, false)
	admin.Publish("jobs", map[string]int{"running": 1}, true)

	select {
	case ev := <-mediaSub.C:
		if ev.Name != "notification" || string(ev.Data) != `{"title":"New titles"}` {
			t.Fatalf("media hub got %s %s", ev.Name, ev.Data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the notification never reached the other hub")
	}

	// The sender's own subscriber sees each event exactly once.
	got := map[string]int{}
	timeout := time.After(time.Second)
	for done := false; !done; {
		select {
		case ev := <-adminSub.C:
			got[ev.Name]++
		case ev := <-mediaSub.C:
			t.Errorf("unexpected relay of %q to the media hub", ev.Name)
		case <-timeout:
			done = true
		}
	}
	if got["notification"] != 1 || got["jobs"] != 1 {
		t.Errorf("sender hub events = %v, want one of each", got)
	}
}
