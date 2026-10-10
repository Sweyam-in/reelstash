package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Webhook is a configured delivery target.
type Webhook struct {
	ID     int64
	Name   string
	URL    string
	Secret string
	Events []string
}

var webhookClient = &http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// retryDelays is how long to wait before each retry after a failed delivery.
var retryDelays = []time.Duration{5 * time.Second, 30 * time.Second}

// ValidateURL accepts only absolute http(s) URLs.
func ValidateURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("webhook URL must start with http:// or https://")
	}
	return nil
}

// KnownKind reports whether kind is an event webhooks can subscribe to.
func KnownKind(kind string) bool {
	for _, k := range Kinds {
		if string(k.Kind) == kind {
			return true
		}
	}
	return false
}

func (s *Service) dispatchWebhooks(ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := s.DB.Query(ctx, `
		SELECT id, name, url, COALESCE(secret, ''), events FROM webhook
		WHERE enabled AND (cardinality(events) = 0 OR $1 = ANY(events))
	`, string(ev.Kind))
	if err != nil {
		log.Printf("notify: load webhooks: %v", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var w Webhook
		if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Secret, &w.Events); err != nil {
			continue
		}
		go s.deliverWithRetry(w, ev)
	}
}

// Test sends a sample event to one webhook and reports the outcome.
func (s *Service) Test(ctx context.Context, w Webhook) (int, error) {
	started := time.Now()
	status, err := send(ctx, w, Event{Kind: webhookTestEvent, Title: "ReelStash test", Body: "This is a test delivery from ReelStash."})
	s.record(w.ID, string(webhookTestEvent), status, err, time.Since(started))
	return status, err
}

func (s *Service) deliverWithRetry(w Webhook, ev Event) {
	var status int
	var err error
	started := time.Now()
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		status, err = send(ctx, w, ev)
		cancel()
		if err == nil || attempt >= len(retryDelays) {
			break
		}
		time.Sleep(retryDelays[attempt])
	}
	s.record(w.ID, string(ev.Kind), status, err, time.Since(started))
}

// record keeps the last outcome on the webhook and a short history (the latest 50 deliveries).
func (s *Service) record(id int64, kind string, status int, err error, took time.Duration) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.DB.Exec(ctx, `
		UPDATE webhook SET last_status = NULLIF($2, 0), last_error = NULLIF($3, ''), last_delivery_at = now() WHERE id = $1
	`, id, status, msg)
	s.DB.Exec(ctx, `
		INSERT INTO webhook_delivery (webhook_id, kind, status, error, duration_ms) VALUES ($1, $2, NULLIF($3, 0), NULLIF($4, ''), $5)
	`, id, kind, status, msg, took.Milliseconds())
	s.DB.Exec(ctx, `
		DELETE FROM webhook_delivery WHERE webhook_id = $1 AND id NOT IN (SELECT id FROM webhook_delivery WHERE webhook_id = $1 ORDER BY id DESC LIMIT 50)
	`, id)
}

// Body builds the request body for a target. Discord and Slack incoming
// webhooks only understand their own shapes, so those get a plain message.
func Body(target string, ev Event, at time.Time) ([]byte, error) {
	parsed, _ := url.Parse(target)
	host := ""
	path := ""
	if parsed != nil {
		host, path = strings.ToLower(parsed.Hostname()), parsed.Path
	}
	text := "**" + ev.Title + "**"
	if ev.Body != "" {
		text += "\n" + ev.Body
	}
	switch {
	case (host == "discord.com" || host == "discordapp.com") && strings.Contains(path, "/api/webhooks/"):
		return json.Marshal(map[string]string{"content": text})
	case host == "hooks.slack.com":
		return json.Marshal(map[string]string{"text": strings.ReplaceAll(text, "**", "*")})
	}
	return json.Marshal(struct {
		Event
		Timestamp string `json:"timestamp"`
	}{ev, at.UTC().Format(time.RFC3339)})
}

// Sign returns the X-ReelStash-Signature value for body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func send(ctx context.Context, w Webhook, ev Event) (int, error) {
	body, err := Body(w.URL, ev, time.Now())
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ReelStash-Webhook/1")
	req.Header.Set("X-ReelStash-Event", string(ev.Kind))
	if w.Secret != "" {
		req.Header.Set("X-ReelStash-Signature", Sign(w.Secret, body))
	}
	resp, err := webhookClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("endpoint answered %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}
