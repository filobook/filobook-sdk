package filobook

// Feed webhooks: FiloBook POSTs new orders to a provider's webhook_url with
//   x-filobook-event: orders.created | test
//   x-filobook-timestamp: unix seconds
//   x-filobook-signature: "sha256=" + hex(HMAC-SHA256(webhook_secret, timestamp + "." + raw_body))

import (
	"bytes"
	"crypto/hmac"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

// Webhook headers.
const (
	SignatureHeader = "X-Filobook-Signature"
	TimestampHeader = "X-Filobook-Timestamp"
	EventHeader     = "X-Filobook-Event"
)

// DefaultWebhookTolerance is the default allowed clock difference between FiloBook and you.
const DefaultWebhookTolerance = 300 * time.Second

var (
	timestampRe = regexp.MustCompile(`^[0-9]{1,12}$`)
	signatureRe = regexp.MustCompile(`^sha256=[0-9a-f]{64}$`)
)

// SignWebhook returns the x-filobook-signature value for a body. Useful for tests and for replaying a
// delivery yourself.
func SignWebhook(secret, timestamp string, body []byte) string {
	msg := make([]byte, 0, len(timestamp)+1+len(body))
	msg = append(msg, timestamp...)
	msg = append(msg, '.')
	msg = append(msg, body...)
	return "sha256=" + HMACSHA256Hex(secret, msg)
}

type webhookConfig struct {
	tolerance time.Duration
	now       time.Time
}

// WebhookOption changes how a webhook is verified.
type WebhookOption func(*webhookConfig)

// WithTolerance sets the allowed clock difference (default 300 s). It is compared in whole seconds.
func WithTolerance(d time.Duration) WebhookOption {
	return func(c *webhookConfig) { c.tolerance = d }
}

// WithNow fixes the current time, for tests. The default is time.Now().
func WithNow(t time.Time) WebhookOption {
	return func(c *webhookConfig) { c.now = t }
}

// VerifyWebhook checks a delivery and returns the parsed event. body must be the raw request body exactly as
// received; never re-serialise parsed JSON. timestamp and signature are the header values ("" if missing).
// Failures are *WebhookError: malformed, stale_timestamp or bad_signature.
func VerifyWebhook(secret string, body []byte, timestamp, signature string, opts ...WebhookOption) (*FeedEvent, error) {
	cfg := webhookConfig{tolerance: DefaultWebhookTolerance}
	for _, o := range opts {
		o(&cfg)
	}
	if !timestampRe.MatchString(timestamp) {
		return nil, &WebhookError{Code: WebhookMalformed, Message: "missing or invalid x-filobook-timestamp"}
	}
	if !signatureRe.MatchString(signature) {
		return nil, &WebhookError{Code: WebhookMalformed, Message: "missing or invalid x-filobook-signature"}
	}
	ts, _ := strconv.ParseInt(timestamp, 10, 64) // at most 12 digits, cannot overflow
	now := cfg.now
	if now.IsZero() {
		now = time.Now()
	}
	diff := now.Unix() - ts
	if diff < 0 {
		diff = -diff
	}
	if diff > int64(cfg.tolerance/time.Second) {
		return nil, &WebhookError{Code: WebhookStaleTimestamp, Message: "timestamp is outside the allowed window"}
	}
	expected := SignWebhook(secret, timestamp, body)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return nil, &WebhookError{Code: WebhookBadSignature, Message: "signature does not match"}
	}
	var ev FeedEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		return nil, &WebhookError{Code: WebhookMalformed, Message: "body is not a JSON feed event"}
	}
	return &ev, nil
}

// VerifyWebhookRequest is VerifyWebhook for an incoming *http.Request. It reads the whole body and then puts
// it back on r.Body, so the handler can read it again. Limit the size first if you need to, e.g. with
// http.MaxBytesReader; a read error is returned as is (not as a *WebhookError).
func VerifyWebhookRequest(r *http.Request, secret string, opts ...WebhookOption) (*FeedEvent, error) {
	var body []byte
	if r.Body != nil {
		var err error
		body, err = io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			return nil, err
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	return VerifyWebhook(secret, body, r.Header.Get(TimestampHeader), r.Header.Get(SignatureHeader), opts...)
}
