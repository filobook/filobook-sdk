package filobook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the live API.
	DefaultBaseURL = "https://dapp.filobook.run"
	// Version is this SDK's version.
	Version = "0.1.1"
	// DefaultTimeout is the per-request timeout.
	DefaultTimeout = 15 * time.Second
)

// Client talks to the FiloBook HTTP API. It is safe for concurrent use.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	timeout    time.Duration
	userAgent  string
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key (fb_live_…), created in the dapp under API keys. It is needed for anything that
// posts or reads "me", and acts as the wallet that created it.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithBaseURL points the client at another deployment. Trailing slashes are removed.
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient replaces http.DefaultClient.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.httpClient = h } }

// WithTimeout sets the per-request timeout (default 15 s). Zero or less disables it; the context still applies.
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.timeout = d } }

// WithUserAgent replaces the default user agent, filobook-sdk-go/<Version>.
func WithUserAgent(ua string) Option { return func(c *Client) { c.userAgent = ua } }

// NewClient returns a client for the live API unless WithBaseURL says otherwise.
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:   DefaultBaseURL,
		timeout:   DefaultTimeout,
		userAgent: "filobook-sdk-go/" + Version,
	}
	for _, o := range opts {
		o(c)
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	return c
}

// BaseURL returns the API base URL, without a trailing slash.
func (c *Client) BaseURL() string { return c.baseURL }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("filobook: bad URL: %w", err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	var reader io.Reader
	if body != nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			return fmt.Errorf("filobook: encode request: %w", err)
		}
		reader = bytes.NewReader(bytes.TrimRight(buf.Bytes(), "\n"))
	}
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return fmt.Errorf("filobook: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("filobook: read response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		apiErr := &APIError{Status: res.StatusCode, Code: "http_error", Message: "HTTP " + strconv.Itoa(res.StatusCode)}
		var e struct {
			Error   *string `json:"error"`
			Message *string `json:"message"`
		}
		if json.Unmarshal(data, &e) == nil {
			if e.Error != nil {
				apiErr.Code = *e.Error
			}
			if e.Message != nil {
				apiErr.Message = *e.Message
			}
		}
		return apiErr
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("filobook: decode %s %s response: %w", method, path, err)
	}
	return nil
}

func get[T any](ctx context.Context, c *Client, path string, q url.Values) (*T, error) {
	var out T
	if err := c.do(ctx, http.MethodGet, path, q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func send[T any](ctx context.Context, c *Client, method, path string, body any) (*T, error) {
	var out T
	if err := c.do(ctx, method, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func seg(id string) string { return url.PathEscape(id) }

func limitQuery(limit int) url.Values {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	return q
}

// ListParams pages a list. Limit 0 lets the server choose.
type ListParams struct {
	Limit int
}

// BookParams filters the book. Zero values are left out.
type BookParams struct {
	Type  RequestType
	Limit int
}

// LedgerParams pages the ledger. Without After, entries come newest first; with After (a height, 0 included),
// oldest first from that height.
type LedgerParams struct {
	After *int64
	Limit int
}

// Price windows.
const (
	Window24h = "24h"
	Window7d  = "7d"
)

// PricesParams filters Prices. Zero values are left out.
type PricesParams struct {
	Type   RequestType
	Window string
}

// FeedParams pages the provider feed. Since is the cursor from the previous call ("" for none).
type FeedParams struct {
	Since string
	Limit int
}

// ---------- public reads ----------

// Health reports whether the API is up.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	return get[Health](ctx, c, "/api/health", nil)
}

// Book returns open orders, newest first.
func (c *Client) Book(ctx context.Context, p BookParams) ([]Order, error) {
	q := limitQuery(p.Limit)
	if p.Type != "" {
		q.Set("type", string(p.Type))
	}
	r, err := get[struct {
		Orders []Order `json:"orders"`
	}](ctx, c, "/v1/book", q)
	if err != nil {
		return nil, err
	}
	return r.Orders, nil
}

// Order returns one order with every quote (ranked by price) and its fill, if any.
func (c *Client) Order(ctx context.Context, id string) (*OrderDetail, error) {
	return get[OrderDetail](ctx, c, "/v1/orders/"+seg(id), nil)
}

// Fills returns recent fills.
func (c *Client) Fills(ctx context.Context, p ListParams) ([]Fill, error) {
	r, err := get[struct {
		Fills []Fill `json:"fills"`
	}](ctx, c, "/v1/fills", limitQuery(p.Limit))
	if err != nil {
		return nil, err
	}
	return r.Fills, nil
}

// Fill returns one fill.
func (c *Client) Fill(ctx context.Context, id string) (*Fill, error) {
	return get[Fill](ctx, c, "/v1/fills/"+seg(id), nil)
}

// Ledger returns ledger entries: newest first, or oldest first from p.After when it is set.
func (c *Client) Ledger(ctx context.Context, p LedgerParams) ([]LedgerEntry, error) {
	q := limitQuery(p.Limit)
	if p.After != nil {
		q.Set("after", strconv.FormatInt(*p.After, 10))
	}
	r, err := get[struct {
		Entries []LedgerEntry `json:"entries"`
	}](ctx, c, "/v1/ledger", q)
	if err != nil {
		return nil, err
	}
	return r.Entries, nil
}

// LedgerServerCheck returns the server's own walk of the chain. VerifyLedger does the same walk locally,
// without trusting the server.
func (c *Client) LedgerServerCheck(ctx context.Context) (*LedgerServerCheck, error) {
	return get[LedgerServerCheck](ctx, c, "/v1/ledger/verify", nil)
}

// Prices returns filled prices over a window, with the median.
func (c *Client) Prices(ctx context.Context, p PricesParams) (*Prices, error) {
	q := url.Values{}
	if p.Type != "" {
		q.Set("type", string(p.Type))
	}
	if p.Window != "" {
		q.Set("window", p.Window)
	}
	return get[Prices](ctx, c, "/v1/prices", q)
}

// Providers returns every listed provider.
func (c *Client) Providers(ctx context.Context) ([]Provider, error) {
	r, err := get[struct {
		Providers []Provider `json:"providers"`
	}](ctx, c, "/v1/providers", nil)
	if err != nil {
		return nil, err
	}
	return r.Providers, nil
}

// ---------- agents ----------

// CreateOrder posts an order. Needs an API key.
func (c *Client) CreateOrder(ctx context.Context, in CreateOrderInput) (*Order, error) {
	return send[Order](ctx, c, http.MethodPost, "/v1/orders", in)
}

// CreateBatch posts up to 20 orders at once; all are validated first, and either every order is created or
// none.
func (c *Client) CreateBatch(ctx context.Context, in CreateBatchInput) (*Batch, error) {
	return send[Batch](ctx, c, http.MethodPost, "/v1/orders/batch", in)
}

// Batch returns one batch with its orders.
func (c *Client) Batch(ctx context.Context, id string) (*Batch, error) {
	return get[Batch](ctx, c, "/v1/batches/"+seg(id), nil)
}

// MyBatches returns your batches.
func (c *Client) MyBatches(ctx context.Context, p ListParams) ([]BatchListItem, error) {
	r, err := get[struct {
		Batches []BatchListItem `json:"batches"`
	}](ctx, c, "/v1/me/batches", limitQuery(p.Limit))
	if err != nil {
		return nil, err
	}
	return r.Batches, nil
}

// CancelBatch cancels every open order in a batch.
func (c *Client) CancelBatch(ctx context.Context, id string) (*Batch, error) {
	return send[Batch](ctx, c, http.MethodPost, "/v1/batches/"+seg(id)+"/cancel", nil)
}

// FillBatch fills every open order in the batch with its cheapest verified quote.
func (c *Client) FillBatch(ctx context.Context, id string) (*FillBatchResult, error) {
	return send[FillBatchResult](ctx, c, http.MethodPost, "/v1/batches/"+seg(id)+"/fill", nil)
}

// CancelOrder cancels an open order.
func (c *Client) CancelOrder(ctx context.Context, id string) (*Order, error) {
	return send[Order](ctx, c, http.MethodPost, "/v1/orders/"+seg(id)+"/cancel", nil)
}

// FillOrder fills now with the cheapest verified quote at or under the max. The API answers 409 with code
// no_verified_quote or conditions_not_met when it cannot.
func (c *Client) FillOrder(ctx context.Context, id string) (*Fill, error) {
	return send[Fill](ctx, c, http.MethodPost, "/v1/orders/"+seg(id)+"/fill", nil)
}

// MyOrders returns your orders.
func (c *Client) MyOrders(ctx context.Context, p ListParams) ([]Order, error) {
	r, err := get[struct {
		Orders []Order `json:"orders"`
	}](ctx, c, "/v1/me/orders", limitQuery(p.Limit))
	if err != nil {
		return nil, err
	}
	return r.Orders, nil
}

// MyFills returns fills where you were the agent or the provider.
func (c *Client) MyFills(ctx context.Context, p ListParams) ([]Fill, error) {
	r, err := get[struct {
		Fills []Fill `json:"fills"`
	}](ctx, c, "/v1/me/fills", limitQuery(p.Limit))
	if err != nil {
		return nil, err
	}
	return r.Fills, nil
}

// ---------- providers ----------

// RegisterProvider registers the key's wallet as a provider.
func (c *Client) RegisterProvider(ctx context.Context, in ProviderInput) (*ProviderMe, error) {
	return send[ProviderMe](ctx, c, http.MethodPost, "/v1/providers", in)
}

// MyProvider returns your provider, including the webhook secret.
func (c *Client) MyProvider(ctx context.Context) (*ProviderMe, error) {
	return get[ProviderMe](ctx, c, "/v1/providers/me", nil)
}

// UpdateProvider changes the fields set in patch. RemoveWebhook removes the webhook and its secret.
func (c *Client) UpdateProvider(ctx context.Context, patch ProviderPatch) (*ProviderMe, error) {
	return send[ProviderMe](ctx, c, http.MethodPatch, "/v1/providers/me", patch)
}

// RotateWebhookSecret issues a new webhook secret.
func (c *Client) RotateWebhookSecret(ctx context.Context) (*ProviderMe, error) {
	return send[ProviderMe](ctx, c, http.MethodPost, "/v1/providers/me/webhook/rotate", nil)
}

// TestWebhook sends a signed "test" event to your webhook now.
func (c *Client) TestWebhook(ctx context.Context) (*WebhookTestResult, error) {
	return send[WebhookTestResult](ctx, c, http.MethodPost, "/v1/providers/me/webhook/test", nil)
}

// Feed returns open orders you can quote. With p.Since (the cursor from the previous call), only newer
// orders, oldest first; poll it alongside the webhook, which is best effort.
func (c *Client) Feed(ctx context.Context, p FeedParams) (*FeedPage, error) {
	q := limitQuery(p.Limit)
	if p.Since != "" {
		q.Set("since", p.Since)
	}
	return get[FeedPage](ctx, c, "/v1/feed", q)
}

// Quote quotes on an order. A re-quote must be strictly lower than your current verified quote.
func (c *Client) Quote(ctx context.Context, orderID string, in QuoteInput) (*Quote, error) {
	return send[Quote](ctx, c, http.MethodPost, "/v1/orders/"+seg(orderID)+"/quotes", in)
}

// MyQuotes returns your quotes.
func (c *Client) MyQuotes(ctx context.Context, p ListParams) ([]Quote, error) {
	r, err := get[struct {
		Quotes []Quote `json:"quotes"`
	}](ctx, c, "/v1/me/quotes", limitQuery(p.Limit))
	if err != nil {
		return nil, err
	}
	return r.Quotes, nil
}

// ---------- ledger ----------

// VerifyLedgerOptions tunes VerifyLedger. Zero values pick the defaults.
type VerifyLedgerOptions struct {
	// PageSize is how many entries to fetch per page (default 500).
	PageSize int
	// Concurrency is how many fills to fetch at once (default 8).
	Concurrency int
	// OnProgress is called with the verified height after each page.
	OnProgress func(height int64)
}

// VerifyLedger walks the whole ledger from genesis and recomputes every hash locally, the same check as
// "Verify chain" in the dapp. It fetches PageSize entries at a time plus the fill behind each one; a 404 on a
// fill is reported as missing_fill. Network and other API errors are returned as errors.
func (c *Client) VerifyLedger(ctx context.Context, opts VerifyLedgerOptions) (ChainResult, error) {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 500
	}
	workers := opts.Concurrency
	if workers <= 0 {
		workers = 8
	}
	pos := Genesis
	for {
		entries, err := c.Ledger(ctx, LedgerParams{After: Int64(pos.Height), Limit: pageSize})
		if err != nil {
			return ChainResult{}, err
		}
		if len(entries) == 0 {
			return ChainResult{OK: true, Height: pos.Height, Head: pos.Head}, nil
		}
		fills, err := c.fetchFills(ctx, entries, workers)
		if err != nil {
			return ChainResult{}, err
		}
		res := VerifyEntries(entries, fills, pos)
		if !res.OK {
			return res, nil
		}
		pos = ChainPosition{Height: res.Height, Head: res.Head}
		if opts.OnProgress != nil {
			opts.OnProgress(pos.Height)
		}
	}
}

func (c *Client) fetchFills(ctx context.Context, entries []LedgerEntry, workers int) (map[string]*Fill, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		fills    = make(map[string]*Fill, len(entries))
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, workers)
	)
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if ctx.Err() != nil {
			break
		}
		if seen[e.Ref] {
			continue
		}
		seen[e.Ref] = true
		ref := e.Ref
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			f, err := c.Fill(ctx, ref)
			var apiErr *APIError
			if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
				f, err = nil, nil
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				return
			}
			fills[ref] = f
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return fills, nil
}
