// Package webhook signs and delivers events to Multica's webhook endpoint.
//
// Multica validates X-Hub-Signature-256 as an HMAC-SHA256 over the raw body,
// keyed by GITHUB_WEBHOOK_SECRET. The signed bytes must be exactly the bytes on
// the wire, so the payload is serialized once and that same buffer is signed
// and sent.
package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client delivers events to a Multica webhook endpoint.
type Client struct {
	Endpoint string
	Secret   string
	HTTP     *http.Client
}

func New(endpoint, secret string) *Client {
	return &Client{
		Endpoint: endpoint,
		Secret:   secret,
		HTTP:     &http.Client{Timeout: 20 * time.Second},
	}
}

// Sign returns the X-Hub-Signature-256 header value for a body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Send serializes, signs and delivers the event. Its success is not the last
// word: the endpoint answers 200 even when it drops the event for an
// unrecognized installation, which callers rule out beforehand. See
// cli.newSender.
func (c *Client) Send(event string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gh-multica-sync")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", deliveryID())
	req.Header.Set("X-Hub-Signature-256", Sign(c.Secret, body))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("delivering webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("webhook rejected with HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}

// deliveryID generates the delivery identifier. Multica does not interpret it,
// but a unique value per delivery keeps the server log readable when several go
// out together.
func deliveryID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("gh-multica-sync-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
