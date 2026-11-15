package filobook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorded struct {
	Method string
	Path   string // escaped path
	Query  string
	Header http.Header
	Body   string
}

type mockServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls []recorded
}

func newMock(t *testing.T, handler func(r *http.Request) (int, any)) *mockServer {
	t.Helper()
	m := &mockServer{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.calls = append(m.calls, recorded{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Clone(), string(body)})
		m.mu.Unlock()
		status, out := handler(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if s, ok := out.(string); ok {
			io.WriteString(w, s)
			return
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(m.Close)
	return m
}

func (m *mockServer) last(t *testing.T) recorded {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		t.Fatal("no calls")
	}
	return m.calls[len(m.calls)-1]
}

var ctx = context.Background()

func TestClientSendsKeyBodyAndHeaders(t *testing.T) {
	m := newMock(t, func(*http.Request) (int, any) {
		return 201, map[string]any{"id": "ord_1", "max_price": "0.05", "rule": nil, "best_quote": "0.04"}
	})
	fb := NewClient(WithAPIKey("fb_live_test"), WithBaseURL(m.URL+"//"))
	if fb.BaseURL() != m.URL {
		t.Errorf("BaseURL = %s", fb.BaseURL())
	}
	o, err := fb.CreateOrder(ctx, CreateOrderInput{
		OrderInput:   OrderInput{Request: "r <&>", Type: TypeWebSearch, MaxPrice: "0.05"},
		QuoteWindowS: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if o.ID != "ord_1" || o.Rule != nil || o.BestQuote == nil || *o.BestQuote != "0.04" {
		t.Errorf("order = %+v", o)
	}
	c := m.last(t)
	if c.Method != "POST" || c.Path != "/v1/orders" || c.Query != "" {
		t.Errorf("call = %s %s?%s", c.Method, c.Path, c.Query)
	}
	if got := c.Header.Get("Authorization"); got != "Bearer fb_live_test" {
		t.Errorf("Authorization = %q", got)
	}
	if got := c.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := c.Header.Get("User-Agent"); got != "filobook-sdk-go/0.1.1" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := c.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	want := `{"request":"r <&>","type":"web_search","max_price":"0.05","quote_window_s":30}`
	if c.Body != want {
		t.Errorf("body = %s\nwant %s", c.Body, want)
	}
}

func TestClientQueryAndListUnwrapping(t *testing.T) {
	m := newMock(t, func(*http.Request) (int, any) {
		return 200, map[string]any{"orders": []map[string]any{{"id": "a"}}, "cursor": nil}
	})
	fb := NewClient(WithBaseURL(m.URL))

	orders, err := fb.Book(ctx, BookParams{Type: TypeInference})
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].ID != "a" {
		t.Errorf("orders = %+v", orders)
	}
	c := m.last(t)
	if c.Path != "/v1/book" || c.Query != "type=inference" {
		t.Errorf("book call = %s?%s", c.Path, c.Query)
	}
	if _, ok := c.Header["Authorization"]; ok {
		t.Error("Authorization sent without a key")
	}
	if _, ok := c.Header["Content-Type"]; ok {
		t.Error("Content-Type sent on a GET")
	}

	if _, err := fb.Book(ctx, BookParams{}); err != nil {
		t.Fatal(err)
	}
	if q := m.last(t).Query; q != "" {
		t.Errorf("empty params sent query %q", q)
	}

	page, err := fb.Feed(ctx, FeedParams{Since: "c_1", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if page.Cursor != nil || len(page.Orders) != 1 {
		t.Errorf("feed = %+v", page)
	}
	if q := m.last(t).Query; q != "limit=5&since=c_1" {
		t.Errorf("feed query = %q", q)
	}

	if _, err := fb.Ledger(ctx, LedgerParams{After: Int64(0)}); err != nil {
		t.Fatal(err)
	}
	if q := m.last(t).Query; q != "after=0" {
		t.Errorf("ledger query = %q (after=0 must be sent)", q)
	}
	if _, err := fb.Ledger(ctx, LedgerParams{}); err != nil {
		t.Fatal(err)
	}
	if q := m.last(t).Query; q != "" {
		t.Errorf("ledger without after sent %q", q)
	}

	if _, err := fb.Prices(ctx, PricesParams{Window: Window7d}); err != nil {
		t.Fatal(err)
	}
	if q := m.last(t).Query; q != "window=7d" {
		t.Errorf("prices query = %q", q)
	}
}

func TestClientErrors(t *testing.T) {
	m := newMock(t, func(r *http.Request) (int, any) {
		switch r.URL.Path {
		case "/v1/orders/ord_1/quotes":
			return 429, map[string]any{"error": "rate_limited", "message": "Too many requests"}
		case "/v1/orders/ord_2/fill":
			return 409, map[string]any{"error": "no_verified_quote", "message": "No verified quote"}
		default:
			return 502, "<html>Bad gateway</html>"
		}
	})
	fb := NewClient(WithBaseURL(m.URL))

	_, err := fb.Quote(ctx, "ord_1", QuoteInput{Price: "0.01"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if *apiErr != (APIError{Status: 429, Code: "rate_limited", Message: "Too many requests"}) {
		t.Errorf("err = %+v", apiErr)
	}
	if m.last(t).Body != `{"price":"0.01"}` {
		t.Errorf("quote body = %s", m.last(t).Body)
	}

	_, err = fb.FillOrder(ctx, "ord_2")
	if !errors.As(err, &apiErr) || apiErr.Status != 409 || apiErr.Code != "no_verified_quote" {
		t.Errorf("fill err = %v", err)
	}

	_, err = fb.Health(ctx)
	if !errors.As(err, &apiErr) || *apiErr != (APIError{Status: 502, Code: "http_error", Message: "HTTP 502"}) {
		t.Errorf("non-JSON err = %v", err)
	}
}

func TestClientEndpoints(t *testing.T) {
	m := newMock(t, func(r *http.Request) (int, any) { return 200, map[string]any{} })
	fb := NewClient(WithBaseURL(m.URL), WithAPIKey("fb_live_x"))
	cases := []struct {
		method, path, query, body string
		call                      func() error
	}{
		{"GET", "/api/health", "", "", func() error { _, e := fb.Health(ctx); return e }},
		{"GET", "/v1/orders/ord%2F1", "", "", func() error { _, e := fb.Order(ctx, "ord/1"); return e }},
		{"GET", "/v1/fills", "limit=3", "", func() error { _, e := fb.Fills(ctx, ListParams{Limit: 3}); return e }},
		{"GET", "/v1/fills/fil_1", "", "", func() error { _, e := fb.Fill(ctx, "fil_1"); return e }},
		{"GET", "/v1/ledger/verify", "", "", func() error { _, e := fb.LedgerServerCheck(ctx); return e }},
		{"GET", "/v1/providers", "", "", func() error { _, e := fb.Providers(ctx); return e }},
		{"POST", "/v1/orders/batch", "", `{"orders":[{"request":"a","type":"tool","max_price":"1","conditions":{"min_quotes":2,"instant_fill_at":"0.5"}}]}`, func() error {
			_, e := fb.CreateBatch(ctx, CreateBatchInput{Orders: []OrderInput{{Request: "a", Type: TypeTool, MaxPrice: "1", Conditions: &Conditions{MinQuotes: 2, InstantFillAt: "0.5"}}}})
			return e
		}},
		{"GET", "/v1/batches/bat_1", "", "", func() error { _, e := fb.Batch(ctx, "bat_1"); return e }},
		{"GET", "/v1/me/batches", "", "", func() error { _, e := fb.MyBatches(ctx, ListParams{}); return e }},
		{"POST", "/v1/batches/bat_1/cancel", "", "", func() error { _, e := fb.CancelBatch(ctx, "bat_1"); return e }},
		{"POST", "/v1/batches/bat_1/fill", "", "", func() error { _, e := fb.FillBatch(ctx, "bat_1"); return e }},
		{"POST", "/v1/orders/ord_1/cancel", "", "", func() error { _, e := fb.CancelOrder(ctx, "ord_1"); return e }},
		{"POST", "/v1/orders/ord_1/fill", "", "", func() error { _, e := fb.FillOrder(ctx, "ord_1"); return e }},
		{"GET", "/v1/me/orders", "limit=10", "", func() error { _, e := fb.MyOrders(ctx, ListParams{Limit: 10}); return e }},
		{"GET", "/v1/me/fills", "", "", func() error { _, e := fb.MyFills(ctx, ListParams{}); return e }},
		{"POST", "/v1/providers", "", `{"name":"P","endpoint_url":"https://p.example","types":["inference"],"active":true}`, func() error {
			_, e := fb.RegisterProvider(ctx, ProviderInput{Name: "P", EndpointURL: "https://p.example", Types: []RequestType{TypeInference}, Active: Bool(true)})
			return e
		}},
		{"GET", "/v1/providers/me", "", "", func() error { _, e := fb.MyProvider(ctx); return e }},
		{"PATCH", "/v1/providers/me", "", `{"active":false,"webhook_url":null}`, func() error {
			_, e := fb.UpdateProvider(ctx, ProviderPatch{Active: Bool(false), WebhookURL: String("ignored"), RemoveWebhook: true})
			return e
		}},
		{"PATCH", "/v1/providers/me", "", `{"name":"Q","webhook_url":"https://q.example/hook"}`, func() error {
			_, e := fb.UpdateProvider(ctx, ProviderPatch{Name: String("Q"), WebhookURL: String("https://q.example/hook")})
			return e
		}},
		{"POST", "/v1/providers/me/webhook/rotate", "", "", func() error { _, e := fb.RotateWebhookSecret(ctx); return e }},
		{"POST", "/v1/providers/me/webhook/test", "", "", func() error { _, e := fb.TestWebhook(ctx); return e }},
		{"POST", "/v1/orders/ord_1/quotes", "", `{"price":"0.02","ttl_s":120,"private":true}`, func() error {
			_, e := fb.Quote(ctx, "ord_1", QuoteInput{Price: "0.02", TTLS: 120, Private: true})
			return e
		}},
		{"GET", "/v1/me/quotes", "", "", func() error { _, e := fb.MyQuotes(ctx, ListParams{}); return e }},
	}
	for _, c := range cases {
		if err := c.call(); err != nil {
			t.Errorf("%s %s: %v", c.method, c.path, err)
			continue
		}
		got := m.last(t)
		if got.Method != c.method || got.Path != c.path || got.Query != c.query || got.Body != c.body {
			t.Errorf("got %s %s?%s %s\nwant %s %s?%s %s", got.Method, got.Path, got.Query, got.Body, c.method, c.path, c.query, c.body)
		}
		if got.Header.Get("Authorization") != "Bearer fb_live_x" {
			t.Errorf("%s: no auth header", c.path)
		}
	}
}

func TestClientDecodesShapes(t *testing.T) {
	m := newMock(t, func(*http.Request) (int, any) {
		return 200, `{"id":"ord_1","agent":"0xa","request":"r","type":"inference","max_price":"0.05","rule":null,"status":"filled",
			"quote_count":2,"best_quote":null,"private_quotes":1,"conditions":{"min_quotes":2,"providers":["prv_1"]},"batch_id":"bat_1",
			"close_reason":null,"created_at":"t0","closes_at":"t1",
			"quotes":[{"id":"quo_1","order_id":"ord_1","provider_id":"prv_1","provider_name":"P","price":null,"private":true,"status":"won",
				"verification":{"checks":[{"name":"price_within_max","ok":true,"detail":"ok"}]},"created_at":"t0","expires_at":"t2"}],
			"fill":{"id":"fil_1","order_id":"ord_1","agent":"0xa","provider_id":"prv_1","provider_name":"P","max_price":"0.05","price":"0.04",
				"saved":"0.01","ledger_height":7,"ledger_hash":"ab","created_at":"t3"}}`
	})
	d, err := NewClient(WithBaseURL(m.URL)).Order(ctx, "ord_1")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "ord_1" || d.Status != OrderFilled || d.BestQuote != nil || d.PrivateQuotes != 1 || *d.BatchID != "bat_1" {
		t.Errorf("order = %+v", d.Order)
	}
	if !reflect.DeepEqual(d.Conditions, &Conditions{MinQuotes: 2, Providers: []string{"prv_1"}}) {
		t.Errorf("conditions = %+v", d.Conditions)
	}
	if len(d.Quotes) != 1 || d.Quotes[0].Price != nil || d.Quotes[0].Verification.Checks[0].Name != CheckPriceWithinMax {
		t.Errorf("quotes = %+v", d.Quotes)
	}
	if d.Fill == nil || d.Fill.LedgerHeight != 7 || d.Fill.Price != "0.04" {
		t.Errorf("fill = %+v", d.Fill)
	}
}

func TestClientTimeout(t *testing.T) {
	m := newMock(t, func(r *http.Request) (int, any) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		return 200, map[string]any{"ok": true}
	})
	start := time.Now()
	_, err := NewClient(WithBaseURL(m.URL), WithTimeout(50*time.Millisecond)).Health(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
	if time.Since(start) > time.Second {
		t.Error("timeout not applied")
	}
}

// ledgerServer serves one ledger vector case: /v1/ledger with after/limit, /v1/fills/:id (404 when absent).
func ledgerServer(t *testing.T, entries []LedgerEntry, fills []Fill, fillStatus int) *mockServer {
	byID := fillsByID(fills)
	return newMock(t, func(r *http.Request) (int, any) {
		if r.URL.Path == "/v1/ledger" {
			after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			if err != nil {
				return 400, map[string]any{"error": "bad_request", "message": "after is required here"}
			}
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			out := []LedgerEntry{}
			for _, e := range entries {
				if e.Height > after && len(out) < limit {
					out = append(out, e)
				}
			}
			return 200, map[string]any{"entries": out}
		}
		if fillStatus != 0 {
			return fillStatus, map[string]any{"error": "internal", "message": "boom"}
		}
		id := strings.TrimPrefix(r.URL.Path, "/v1/fills/")
		if f, ok := byID[id]; ok {
			return 200, f
		}
		return 404, map[string]any{"error": "not_found", "message": "Fill not found"}
	})
}

func TestVerifyLedgerPages(t *testing.T) {
	var v ledgerVectors
	loadVectors(t, "ledger.json", &v)
	valid := v.Cases[0]
	m := ledgerServer(t, valid.Entries, valid.Fills, 0)
	var progress []int64
	res, err := NewClient(WithBaseURL(m.URL)).VerifyLedger(ctx, VerifyLedgerOptions{
		PageSize:   2,
		OnProgress: func(h int64) { progress = append(progress, h) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res != valid.Expect {
		t.Errorf("res = %+v, want %+v", res, valid.Expect)
	}
	if !reflect.DeepEqual(progress, []int64{2, 3}) {
		t.Errorf("progress = %v", progress)
	}
	var ledgerQueries []string
	for _, c := range m.calls {
		if c.Path == "/v1/ledger" {
			ledgerQueries = append(ledgerQueries, c.Query)
		}
	}
	want := []string{"after=0&limit=2", "after=2&limit=2", "after=3&limit=2"}
	if !reflect.DeepEqual(ledgerQueries, want) {
		t.Errorf("ledger queries = %v, want %v", ledgerQueries, want)
	}

	// Default page size is 500.
	m2 := ledgerServer(t, valid.Entries, valid.Fills, 0)
	if _, err := NewClient(WithBaseURL(m2.URL)).VerifyLedger(ctx, VerifyLedgerOptions{}); err != nil {
		t.Fatal(err)
	}
	if q := m2.calls[0].Query; q != "after=0&limit=500" {
		t.Errorf("default page query = %q", q)
	}
}

func TestVerifyLedgerMissingFill(t *testing.T) {
	var v ledgerVectors
	loadVectors(t, "ledger.json", &v)
	for _, c := range v.Cases {
		if c.Name != "fill 3 missing" {
			continue
		}
		m := ledgerServer(t, c.Entries, c.Fills, 0)
		res, err := NewClient(WithBaseURL(m.URL)).VerifyLedger(ctx, VerifyLedgerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res != c.Expect || res.Reason != BreakMissingFill {
			t.Errorf("res = %+v, want %+v", res, c.Expect)
		}
		return
	}
	t.Fatal(`vector "fill 3 missing" not found`)
}

func TestVerifyLedgerAllVectorCases(t *testing.T) {
	var v ledgerVectors
	loadVectors(t, "ledger.json", &v)
	for _, c := range v.Cases {
		t.Run(c.Name, func(t *testing.T) {
			m := ledgerServer(t, c.Entries, c.Fills, 0)
			res, err := NewClient(WithBaseURL(m.URL)).VerifyLedger(ctx, VerifyLedgerOptions{PageSize: 1})
			if err != nil {
				t.Fatal(err)
			}
			if res != c.Expect {
				t.Errorf("res = %+v, want %+v", res, c.Expect)
			}
		})
	}
}

func TestVerifyLedgerPropagatesErrors(t *testing.T) {
	var v ledgerVectors
	loadVectors(t, "ledger.json", &v)
	m := ledgerServer(t, v.Cases[0].Entries, v.Cases[0].Fills, 500)
	_, err := NewClient(WithBaseURL(m.URL)).VerifyLedger(ctx, VerifyLedgerOptions{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 500 {
		t.Errorf("err = %v, want APIError 500", err)
	}
}
