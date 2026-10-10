package events

import (
	"testing"
	"time"
)

func TestPublishRespectsAudience(t *testing.T) {
	h := New()
	admin, stopAdmin := h.Subscribe(true)
	defer stopAdmin()
	user, stopUser := h.Subscribe(false)
	defer stopUser()

	h.Publish("jobs", map[string]int{"n": 1}, true)
	h.Publish("notification", map[string]int{"n": 3}, true)
	h.Publish("notification", map[string]int{"n": 2}, false)

	if got := (<-admin.C).Name; got != "jobs" {
		t.Fatalf("admin first event = %q, want jobs", got)
	}
	if ev := <-admin.C; ev.Name != "notification" || string(ev.Data) != `{"n":3}` {
		t.Fatalf("admin second event = %q %s, want admin notification", ev.Name, ev.Data)
	}
	if ev := <-admin.C; ev.Name != "notification" || string(ev.Data) != `{"n":2}` {
		t.Fatalf("admin third event = %q %s, want public notification", ev.Name, ev.Data)
	}
	select {
	case ev := <-user.C:
		if ev.Name != "notification" || string(ev.Data) != `{"n":2}` {
			t.Fatalf("non-admin received %q %s, want only public notification", ev.Name, ev.Data)
		}
	case <-time.After(time.Second):
		t.Fatal("non-admin never received the notification")
	}
	select {
	case ev := <-user.C:
		t.Fatalf("non-admin received extra event %q", ev.Name)
	default:
	}
}

func TestKickCoalesces(t *testing.T) {
	h := New()
	h.Kick()
	h.Kick()
	h.Kick()
	<-h.Kicked()
	select {
	case <-h.Kicked():
		t.Fatal("expected kicks to coalesce into a single wake-up")
	default:
	}
}

func TestUnsubscribeAndCount(t *testing.T) {
	h := New()
	_, stop := h.Subscribe(true)
	if h.AdminSubscribers() != 1 {
		t.Fatal("want 1 admin subscriber")
	}
	stop()
	if h.AdminSubscribers() != 0 {
		t.Fatal("want 0 admin subscribers after unsubscribe")
	}
}
