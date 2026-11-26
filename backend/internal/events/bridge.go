package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// channel is the PostgreSQL NOTIFY channel shared by ReelStash processes.
const channel = "reelstash_events"

// maxPayload stays under PostgreSQL's 8000-byte NOTIFY limit.
const maxPayload = 7900

type wire struct {
	Origin    string          `json:"o"`
	Name      string          `json:"n"`
	AdminOnly bool            `json:"a,omitempty"`
	Data      json.RawMessage `json:"d"`
}

// Bridge connects this process's hub to every other ReelStash process on the
// same PostgreSQL database, so a notification raised on the admin server
// (for example when a scan finds new titles) reaches people connected to the
// media server. Only the named events cross; others stay local. It returns
// once the listener is running and keeps reconnecting until ctx ends.
func Bridge(ctx context.Context, pool *pgxpool.Pool, h *Hub, names ...string) {
	origin := randomID()
	relayed := map[string]bool{}
	for _, n := range names {
		relayed[n] = true
	}
	h.mu.Lock()
	h.relay = func(name string, data []byte, adminOnly bool) {
		if !relayed[name] {
			return
		}
		msg, err := json.Marshal(wire{Origin: origin, Name: name, AdminOnly: adminOnly, Data: data})
		if err != nil || len(msg) > maxPayload {
			log.Printf("events: %q is too large to send to other servers (%d bytes)", name, len(msg))
			return
		}
		c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := pool.Exec(c, `SELECT pg_notify($1, $2)`, channel, string(msg)); err != nil {
			log.Printf("events: send %q to other servers: %v", name, err)
		}
	}
	h.mu.Unlock()
	go listen(ctx, pool, h, origin)
}

func listen(ctx context.Context, pool *pgxpool.Pool, h *Hub, origin string) {
	for ctx.Err() == nil {
		if err := listenOnce(ctx, pool, h, origin); err != nil && ctx.Err() == nil {
			log.Printf("events: listener stopped, reconnecting: %v", err)
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, h *Hub, origin string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+channel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var msg wire
		if json.Unmarshal([]byte(n.Payload), &msg) != nil || msg.Origin == origin {
			continue
		}
		h.Deliver(msg.Name, msg.Data, msg.AdminOnly)
	}
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
