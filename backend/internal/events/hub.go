// Package events is a tiny in-process pub/sub used to push server-sent events
// (job progress, notifications) to connected browsers.
package events

import (
	"encoding/json"
	"sync"
)

// Event is one named message. Data is already-encoded JSON.
type Event struct {
	Name string
	Data []byte
}

// Subscriber receives events on C. Admin subscribers also receive admin-only events.
type Subscriber struct {
	C     chan Event
	admin bool
}

type Hub struct {
	mu   sync.Mutex
	subs map[*Subscriber]struct{}
	kick chan struct{}
}

func New() *Hub {
	return &Hub{subs: map[*Subscriber]struct{}{}, kick: make(chan struct{}, 1)}
}

// Subscribe registers a listener; call the returned function to unsubscribe.
func (h *Hub) Subscribe(admin bool) (*Subscriber, func()) {
	sub := &Subscriber{C: make(chan Event, 64), admin: admin}
	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()
	return sub, func() {
		h.mu.Lock()
		delete(h.subs, sub)
		h.mu.Unlock()
	}
}

// AdminSubscribers reports how many connected listeners are admins.
func (h *Hub) AdminSubscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for sub := range h.subs {
		if sub.admin {
			n++
		}
	}
	return n
}

// Publish sends an event to every subscriber (only admins when adminOnly).
// A subscriber whose buffer is full misses the event rather than blocking the publisher.
func (h *Hub) Publish(name string, payload any, adminOnly bool) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ev := Event{Name: name, Data: data}
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs {
		if adminOnly && !sub.admin {
			continue
		}
		select {
		case sub.C <- ev:
		default:
		}
	}
}

// Kick signals that job state changed. Signals coalesce: any number of kicks
// before the consumer wakes produce a single wake-up.
func (h *Hub) Kick() {
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

// Kicked is the channel the job feed waits on.
func (h *Hub) Kicked() <-chan struct{} { return h.kick }
