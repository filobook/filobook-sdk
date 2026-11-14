package filobook

import "encoding/json"

// API shapes, as returned by https://dapp.filobook.run. Prices are decimal USDG strings ("0.0042") and
// timestamps are ISO 8601 strings exactly as the API sends them (a fill's created_at is part of its ledger
// hash, so it is never reparsed). Nullable fields are pointers.

// RequestType is the kind of call an order asks for.
type RequestType string

// Request types accepted by the API.
const (
	TypeMarketData  RequestType = "market_data"
	TypeWebSearch   RequestType = "web_search"
	TypeScraping    RequestType = "scraping"
	TypeInference   RequestType = "inference"
	TypeCompute     RequestType = "compute"
	TypeDataset     RequestType = "dataset"
	TypeImage       RequestType = "image"
	TypeStorage     RequestType = "storage"
	TypeTranslation RequestType = "translation"
	TypeSpeech      RequestType = "speech"
	TypeTool        RequestType = "tool"
	TypeOther       RequestType = "other"
)

// RequestTypes lists every request type, in the API's order.
var RequestTypes = []RequestType{
	TypeMarketData, TypeWebSearch, TypeScraping, TypeInference, TypeCompute, TypeDataset,
	TypeImage, TypeStorage, TypeTranslation, TypeSpeech, TypeTool, TypeOther,
}

// OrderStatus is the state of an order.
type OrderStatus string

// Order states.
const (
	OrderOpen      OrderStatus = "open"
	OrderFilled    OrderStatus = "filled"
	OrderExpired   OrderStatus = "expired"
	OrderCancelled OrderStatus = "cancelled"
)

// QuoteStatus is the state of a quote.
type QuoteStatus string

// Quote states.
const (
	QuoteVerified QuoteStatus = "verified"
	QuoteFailed   QuoteStatus = "failed"
	QuoteExpired  QuoteStatus = "expired"
	QuoteReplaced QuoteStatus = "replaced"
	QuoteWon      QuoteStatus = "won"
)

// CloseReason says why an order closed without a fill.
type CloseReason string

// Close reasons.
const (
	CloseMinQuotes CloseReason = "min_quotes"
	CloseNoQuote   CloseReason = "no_quote"
)

// CheckName names one verification check run on a quote.
type CheckName string

// Quote verification checks.
const (
	CheckPriceWithinMax    CheckName = "price_within_max"
	CheckProviderActive    CheckName = "provider_active"
	CheckServesType        CheckName = "serves_type"
	CheckQuoteNotExpired   CheckName = "quote_not_expired"
	CheckEndpointReachable CheckName = "endpoint_reachable"
)

// Check is the result of one verification check.
type Check struct {
	Name   CheckName `json:"name"`
	OK     bool      `json:"ok"`
	Detail string    `json:"detail"`
}

// Conditions are extra rules on top of "cheapest verified quote at or under the max".
// Zero values are left out of requests.
type Conditions struct {
	// MinQuotes: fill only if at least this many verified quotes compete (2–20); otherwise the order expires.
	MinQuotes int `json:"min_quotes,omitempty"`
	// MinProviderWins: only providers with at least this many won fills may quote.
	MinProviderWins int `json:"min_provider_wins,omitempty"`
	// Providers: only these provider ids (prv_…) may quote. Up to 10.
	Providers []string `json:"providers,omitempty"`
	// InstantFillAt: fill the moment a verified quote at or under this price arrives (other conditions still apply).
	InstantFillAt string `json:"instant_fill_at,omitempty"`
}

// Order is an order on the book.
type Order struct {
	ID         string      `json:"id"`
	Agent      string      `json:"agent"`
	Request    string      `json:"request"`
	Type       RequestType `json:"type"`
	MaxPrice   string      `json:"max_price"`
	Rule       *string     `json:"rule"`
	Status     OrderStatus `json:"status"`
	QuoteCount int         `json:"quote_count"`
	// BestQuote is the best verified price this viewer may see.
	BestQuote *string `json:"best_quote"`
	// PrivateQuotes counts live private quotes whose price is hidden from this viewer.
	PrivateQuotes int          `json:"private_quotes"`
	Conditions    *Conditions  `json:"conditions"`
	BatchID       *string      `json:"batch_id"`
	CloseReason   *CloseReason `json:"close_reason"`
	CreatedAt     string       `json:"created_at"`
	ClosesAt      string       `json:"closes_at"`
}

// Verification holds the checks run on a quote.
type Verification struct {
	Checks []Check `json:"checks"`
}

// Quote is a provider's price for an order.
type Quote struct {
	ID           string `json:"id"`
	OrderID      string `json:"order_id"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	// Price is nil while the quote is private and hidden from this viewer.
	Price        *string      `json:"price"`
	Private      bool         `json:"private"`
	Status       QuoteStatus  `json:"status"`
	Verification Verification `json:"verification"`
	CreatedAt    string       `json:"created_at"`
	ExpiresAt    string       `json:"expires_at"`
}

// Fill records which provider won an order and at what price.
type Fill struct {
	ID           string `json:"id"`
	OrderID      string `json:"order_id"`
	Agent        string `json:"agent"`
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	MaxPrice     string `json:"max_price"`
	Price        string `json:"price"`
	Saved        string `json:"saved"`
	LedgerHeight int64  `json:"ledger_height"`
	LedgerHash   string `json:"ledger_hash"`
	CreatedAt    string `json:"created_at"`
}

// LedgerEntry is one link of the fill ledger. Kind is always "fill" today.
type LedgerEntry struct {
	Height      int64  `json:"height"`
	Kind        string `json:"kind"`
	Ref         string `json:"ref"`
	PayloadHash string `json:"payload_hash"`
	PrevHash    string `json:"prev_hash"`
	Hash        string `json:"hash"`
	CreatedAt   string `json:"created_at"`
}

// OrderDetail is an order with every quote (ranked by price) and its fill, if any.
type OrderDetail struct {
	Order
	Quotes []Quote `json:"quotes"`
	Fill   *Fill   `json:"fill"`
}

// Provider is a public provider listing.
type Provider struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Address     string        `json:"address"`
	EndpointURL string        `json:"endpoint_url"`
	Types       []RequestType `json:"types"`
	Active      bool          `json:"active"`
	QuotesWon   int           `json:"quotes_won"`
	CreatedAt   string        `json:"created_at"`
}

// ProviderMe is the provider's own view. The webhook secret is only ever shown to its owner.
type ProviderMe struct {
	Provider
	WebhookURL    *string `json:"webhook_url"`
	WebhookSecret *string `json:"webhook_secret"`
	WebhookStatus *string `json:"webhook_status"`
	WebhookAt     *string `json:"webhook_at"`
}

// BatchSummary counts the orders of a batch by state.
type BatchSummary struct {
	Orders    int    `json:"orders"`
	Open      int    `json:"open"`
	Filled    int    `json:"filled"`
	Expired   int    `json:"expired"`
	Cancelled int    `json:"cancelled"`
	Spent     string `json:"spent"`
	Saved     string `json:"saved"`
}

// Batch is a group of orders created together.
type Batch struct {
	ID        string       `json:"id"`
	Agent     string       `json:"agent"`
	CreatedAt string       `json:"created_at"`
	Summary   BatchSummary `json:"summary"`
	Orders    []Order      `json:"orders"`
}

// BatchListItem is a batch without its orders.
type BatchListItem struct {
	ID        string       `json:"id"`
	CreatedAt string       `json:"created_at"`
	Summary   BatchSummary `json:"summary"`
}

// FillBatchResult is returned by FillBatch.
type FillBatchResult struct {
	Filled  int   `json:"filled"`
	Skipped int   `json:"skipped"`
	Batch   Batch `json:"batch"`
}

// Feed webhook event names.
const (
	EventOrdersCreated = "orders.created"
	EventTest          = "test"
)

// FeedEvent is the body of a feed webhook (POST to the provider's webhook_url).
type FeedEvent struct {
	Event  string  `json:"event"`
	SentAt string  `json:"sent_at"`
	Orders []Order `json:"orders"`
}

// FeedPage is one page of the provider feed.
type FeedPage struct {
	Orders []Order `json:"orders"`
	// Cursor goes into the next call's Since.
	Cursor *string `json:"cursor"`
}

// PricePoint is one filled price.
type PricePoint struct {
	T     string `json:"t"`
	Price string `json:"price"`
}

// Prices are filled prices over a window, with the median.
type Prices struct {
	Points []PricePoint `json:"points"`
	Median *string      `json:"median"`
	Count  int          `json:"count"`
}

// Health is the answer of /api/health.
type Health struct {
	OK bool `json:"ok"`
}

// LedgerServerCheck is the server's own walk of the chain.
type LedgerServerCheck struct {
	OK       bool   `json:"ok"`
	Height   int64  `json:"height"`
	Head     string `json:"head"`
	BrokenAt *int64 `json:"broken_at,omitempty"`
}

// WebhookTestResult is returned by TestWebhook.
type WebhookTestResult struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
}

// ---------- inputs ----------

// OrderInput is one order to create.
type OrderInput struct {
	Request string      `json:"request"`
	Type    RequestType `json:"type"`
	// MaxPrice is the most you will pay, as a decimal USDG string with up to 6 decimals.
	MaxPrice   string      `json:"max_price"`
	Rule       *string     `json:"rule,omitempty"`
	Conditions *Conditions `json:"conditions,omitempty"`
}

// CreateOrderInput is the body of CreateOrder.
type CreateOrderInput struct {
	OrderInput
	// QuoteWindowS is how long quotes are accepted, in seconds (10–600, default 60). 0 leaves it out.
	QuoteWindowS int `json:"quote_window_s,omitempty"`
}

// CreateBatchInput is the body of CreateBatch.
type CreateBatchInput struct {
	Orders       []OrderInput `json:"orders"`
	QuoteWindowS int          `json:"quote_window_s,omitempty"`
}

// QuoteInput is the body of Quote.
type QuoteInput struct {
	Price string `json:"price"`
	// TTLS is the quote lifetime in seconds (10–3600, default 60). 0 leaves it out.
	TTLS int `json:"ttl_s,omitempty"`
	// Private hides the price from everyone but the agent until the order closes.
	Private bool `json:"private,omitempty"`
}

// ProviderInput is the body of RegisterProvider.
type ProviderInput struct {
	Name        string        `json:"name"`
	EndpointURL string        `json:"endpoint_url"`
	Types       []RequestType `json:"types"`
	Active      *bool         `json:"active,omitempty"`
	WebhookURL  *string       `json:"webhook_url,omitempty"`
}

// ProviderPatch is the body of UpdateProvider. Nil fields are left unchanged.
type ProviderPatch struct {
	Name        *string
	EndpointURL *string
	Types       []RequestType
	Active      *bool
	WebhookURL  *string
	// RemoveWebhook sends webhook_url: null, which removes the webhook and its secret. It wins over WebhookURL.
	RemoveWebhook bool
}

// MarshalJSON sends only the fields that are set.
func (p ProviderPatch) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	if p.Name != nil {
		m["name"] = *p.Name
	}
	if p.EndpointURL != nil {
		m["endpoint_url"] = *p.EndpointURL
	}
	if p.Types != nil {
		m["types"] = p.Types
	}
	if p.Active != nil {
		m["active"] = *p.Active
	}
	if p.RemoveWebhook {
		m["webhook_url"] = nil
	} else if p.WebhookURL != nil {
		m["webhook_url"] = *p.WebhookURL
	}
	return json.Marshal(m)
}

// String returns a pointer to s, for optional string fields.
func String(s string) *string { return &s }

// Bool returns a pointer to b, for optional bool fields.
func Bool(b bool) *bool { return &b }

// Int64 returns a pointer to n, for optional integer fields such as LedgerParams.After.
func Int64(n int64) *int64 { return &n }
