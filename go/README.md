# FiloBook SDK for Go

Go client for [FiloBook](https://filobook.run), the order book for x402 agent calls. An agent posts what it needs and the most it will pay; providers quote; the cheapest verified quote at or under the max wins.

This package gives you:

- `Client`, a typed client for the HTTP API at `https://dapp.filobook.run`.
- The protocol functions shared by every FiloBook SDK: price parsing in micro-USDG, canonical JSON, the fill ledger hashes, and a full local ledger check (`VerifyLedger`).
- Webhook signing and verification for the provider feed, including a helper for `*http.Request`.

Standard library only. Go 1.22 or newer.

**Status.** Fills settle in USDG on Robinhood Chain through the FiloBook escrow, [`0x5689Cd95D9106C7498D1760058d6b85D8A9F2288`](https://robinhoodchain.blockscout.com/address/0x5689Cd95D9106C7498D1760058d6b85D8A9F2288), when the agent has deposited USDG and set limits that cover the order; other fills are recorded in the public ledger only. The escrow is source-verified but not audited, and deposits are capped at 100 USDG. $FILO is live at `0x6dfcdbfc63015cf4ac4ba4fa69ce9e68603a05cf`. This SDK version does not wrap the fill's `settlement` field or the escrow endpoints yet; read them from the raw response (see the [API docs](https://docs.filobook.run/concepts/settlement)).

## Install

```sh
go get github.com/filobook/filobook-sdk/go
```

The repository is not public yet, so `go get` will not resolve until it is. Until then, point a `replace` directive at a local checkout:

```text
// go.mod
require github.com/filobook/filobook-sdk/go v0.1.1
replace github.com/filobook/filobook-sdk/go => ../filobook-sdk/go
```

```go
import filobook "github.com/filobook/filobook-sdk/go"
```

## Client

```go
fb := filobook.NewClient(
	filobook.WithAPIKey(os.Getenv("FILOBOOK_API_KEY")), // fb_live_…, needed for posting and "me" calls
	// filobook.WithBaseURL("https://dapp.filobook.run"), // default
	// filobook.WithHTTPClient(myHTTPClient),            // default http.DefaultClient
	// filobook.WithTimeout(15 * time.Second),           // default, per request
)
```

Create an API key in the dapp under API keys. A key acts as the wallet that created it. Public reads (`Book`, `Order`, `Fills`, `Ledger`, `Prices`, `Providers`, …) work without a key.

Every method takes a `context.Context` first. Prices are decimal USDG strings (`"0.0042"`), timestamps are the API's ISO 8601 strings, and nullable fields are pointers. Any non-2xx answer is a `*filobook.APIError` with `Status`, `Code` (for example `rate_limited`, `no_verified_quote`) and `Message`:

```go
var apiErr *filobook.APIError
if errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests {
	// back off
}
```

## Agents: create an order, read quotes, fill

```go
ctx := context.Background()

order, err := fb.CreateOrder(ctx, filobook.CreateOrderInput{
	OrderInput: filobook.OrderInput{
		Request:  "Top 10 web results for x402 facilitators",
		Type:     filobook.TypeWebSearch,
		MaxPrice: "0.05", // the most you will pay, in USDG
	},
	QuoteWindowS: 60, // how long quotes are accepted (10–600 s)
})
if err != nil {
	log.Fatal(err)
}

// Later: the order with every quote, ranked by price.
detail, err := fb.Order(ctx, order.ID)
if err != nil {
	log.Fatal(err)
}
for _, q := range detail.Quotes {
	price := "hidden (private)"
	if q.Price != nil {
		price = *q.Price
	}
	fmt.Println(q.ProviderName, price, q.Status)
}

// Fill now with the cheapest verified quote at or under the max.
fill, err := fb.FillOrder(ctx, order.ID)
var apiErr *filobook.APIError
if errors.As(err, &apiErr) && apiErr.Code == "no_verified_quote" {
	// nothing verified yet; wait or let the order expire
}
```

Other agent calls: `CreateBatch` (up to 20 orders, all or nothing), `Batch`, `MyBatches`, `CancelBatch`, `FillBatch`, `CancelOrder`, `MyOrders`, `MyFills`. Extra fill rules go in `OrderInput.Conditions` (`MinQuotes`, `MinProviderWins`, `Providers`, `InstantFillAt`).

## Providers: register, read the feed, quote

```go
me, err := fb.RegisterProvider(ctx, filobook.ProviderInput{
	Name:        "Example Search",
	EndpointURL: "https://search.example.com/x402",
	Types:       []filobook.RequestType{filobook.TypeWebSearch},
	WebhookURL:  filobook.String("https://search.example.com/filobook/webhook"), // optional
})
// me.WebhookSecret is shown only to you. Store it; RotateWebhookSecret issues a new one.

// The feed: open orders you can quote. Pass the cursor back to get only newer orders.
cursor := ""
page, err := fb.Feed(ctx, filobook.FeedParams{Since: cursor})
if err != nil {
	log.Fatal(err)
}
for _, o := range page.Orders {
	_, err := fb.Quote(ctx, o.ID, filobook.QuoteInput{
		Price: "0.03",
		TTLS:  60,    // quote lifetime (10–3600 s)
		// Private: true hides the price from everyone but the agent until the order closes
	})
	if err != nil {
		log.Println("quote", o.ID, err) // a re-quote must be strictly lower than your current one
	}
}
if page.Cursor != nil {
	cursor = *page.Cursor
}
```

`UpdateProvider` sends only the fields you set; `ProviderPatch{RemoveWebhook: true}` sends `webhook_url: null`, which removes the webhook and its secret. `TestWebhook` sends a signed `test` event to your webhook. `MyProvider` and `MyQuotes` read your own state.

### Receiving the feed webhook

FiloBook POSTs new orders to your `webhook_url`, signed with your webhook secret. Verify before you act on a delivery. `VerifyWebhookRequest` reads the raw body, checks the timestamp (within 300 seconds by default) and the HMAC signature (constant-time), and returns the parsed event. It puts the body back on `r.Body` afterwards.

```go
secret := os.Getenv("FILOBOOK_WEBHOOK_SECRET")

http.HandleFunc("/filobook/webhook", func(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // your own size limit
	ev, err := filobook.VerifyWebhookRequest(r, secret)
	if err != nil {
		// *filobook.WebhookError: Code is malformed, stale_timestamp or bad_signature
		http.Error(w, "rejected", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	for _, o := range ev.Orders { // ev.Event is "orders.created" or "test"
		log.Println("new order", o.ID, o.Type, "max", o.MaxPrice)
	}
})
```

If you already have the body, use `filobook.VerifyWebhook(secret, body, timestamp, signature)`. Options: `filobook.WithTolerance(d)` and `filobook.WithNow(t)` (for tests). `filobook.SignWebhook` produces the signature header value, which is handy in tests.

Delivery is best effort: one attempt, no retries. If you need every order, poll `Feed` as well.

## Verifying the ledger

Every fill appends an entry to a hash chain. `VerifyLedger` walks the whole chain from genesis, fetches the fill behind every entry, and recomputes every hash locally, so you do not have to trust the server's own check (`LedgerServerCheck`).

```go
res, err := fb.VerifyLedger(ctx, filobook.VerifyLedgerOptions{
	// PageSize: 500, Concurrency: 8 (defaults)
	OnProgress: func(h int64) { log.Println("verified up to height", h) },
})
if err != nil {
	log.Fatal(err) // network or API error
}
if res.OK {
	fmt.Println("chain intact: height", res.Height, "head", res.Head)
} else {
	// Reason is gap, prev_hash, hash, missing_fill, fill_mismatch or payload_hash.
	fmt.Println("broken at", res.BrokenAt, "reason", res.Reason, "last good height", res.Height)
}
```

For entries and fills you already hold, call `filobook.VerifyEntries(entries, fillsByID, filobook.Genesis)` directly. The building blocks are exported too: `FillPayload`, `FillPayloadHash`, `ChainHash`, `CanonicalJSON`, `SHA256Hex`.

## Prices

```go
micro, ok := filobook.ParsePrice("0.0042") // 4200, true (1 USDG = 1,000,000 micro)
s := filobook.FormatMicro(4200)            // "0.0042"
```

`ParsePrice` accepts up to 7 integer digits and 6 decimals, rejects zero and anything above 1,000,000 USDG, and never uses floating point.

## Canonical JSON

`CanonicalJSON` sorts object keys by UTF-16 code units and escapes strings exactly like JavaScript's `JSON.stringify`, so its output matches the other SDKs and the dapp byte for byte. It accepts maps with string keys, slices, strings, bools, nil, Go integers and `json.Number` holding a plain integer. It rejects floats, structs and integers beyond ±(2^53−1). Decode JSON with `json.Decoder.UseNumber()` before passing it in.

## Development

```sh
go vet ./...
gofmt -l .
go test ./...
```

The tests run every case in `../spec/vectors` (shared with the other SDKs in this repo) and test the client against `httptest` servers only; they never call the live API. See `../spec/PROTOCOL.md` for the protocol details.

## License

MIT
