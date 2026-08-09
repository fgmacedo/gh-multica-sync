package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Known HMAC-SHA256 vector, the same one GitHub's webhook documentation uses.
// If this assertion breaks, the signature is wrong and nothing else matters.
func TestSignKnownVector(t *testing.T) {
	got := Sign("It's a Secret to Everybody", []byte("Hello, World!"))
	want := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
}

// The signature must cover exactly the bytes we send: serializing twice would
// let a field reordering invalidate the delivery.
func TestSendSignsTheBodyItSends(t *testing.T) {
	const secret = "topsecret"
	var gotSig, gotEvent, gotDelivery string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Hub-Signature-256")
		gotEvent = r.Header.Get("X-GitHub-Event")
		gotDelivery = r.Header.Get("X-GitHub-Delivery")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(srv.URL, secret)
	if err := c.Send("pull_request", map[string]any{"action": "opened", "number": 7}); err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if want := Sign(secret, gotBody); gotSig != want {
		t.Fatalf("signature does not match the received body: %q != %q", gotSig, want)
	}
	if gotEvent != "pull_request" {
		t.Fatalf("X-GitHub-Event = %q", gotEvent)
	}
	if len(gotDelivery) != 36 {
		t.Fatalf("X-GitHub-Delivery does not look like a uuid: %q", gotDelivery)
	}
	var decoded map[string]any
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("delivered body is not valid JSON: %v", err)
	}
}

func TestSendPropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github webhooks not configured", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := New(srv.URL, "s").Send("pull_request", map[string]any{})
	if err == nil {
		t.Fatal("want an error on HTTP 503, got nil")
	}
}

func TestDeliveryIDIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := deliveryID()
		if seen[id] {
			t.Fatalf("duplicate delivery id: %s", id)
		}
		seen[id] = true
	}
}
