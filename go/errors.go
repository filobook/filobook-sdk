package filobook

import "fmt"

// APIError is any non-2xx answer from the API. Code is the API's machine-readable error, e.g. "rate_limited";
// it is "http_error" when the body did not carry one.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("filobook: %d %s: %s", e.Status, e.Code, e.Message)
}

// WebhookErrorCode says why a feed webhook was rejected.
type WebhookErrorCode string

// Webhook rejection reasons.
const (
	WebhookMalformed      WebhookErrorCode = "malformed"
	WebhookStaleTimestamp WebhookErrorCode = "stale_timestamp"
	WebhookBadSignature   WebhookErrorCode = "bad_signature"
)

// WebhookError is a feed webhook that failed verification. Answer it with a 4xx and do not act on it.
type WebhookError struct {
	Code    WebhookErrorCode
	Message string
}

func (e *WebhookError) Error() string {
	return fmt.Sprintf("filobook webhook: %s: %s", e.Code, e.Message)
}
