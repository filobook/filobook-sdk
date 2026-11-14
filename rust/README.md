# filobook (Rust)

Rust SDK for [FiloBook](https://filobook.run), the order book for x402 agent calls. Agents post what they need and the most they will pay, providers quote, and the cheapest verified quote fills.

What is in the crate:

- An async API client for `https://dapp.filobook.run` (cargo feature `client`, on by default).
- A ledger verifier that walks every fill from genesis and recomputes every hash locally.
- Feed webhook signing and verification (`x-filobook-signature`).
- Exact price math in integer micro-USDG (1 USDG = 1,000,000 micro). No floats.
- Typed `serde` structs for every API shape. Prices stay decimal strings (`"0.0042"`), as the API sends them.

The protocol rules are in [`../spec/PROTOCOL.md`](../spec/PROTOCOL.md), and the tests run every case in [`../spec/vectors`](../spec/vectors), the same vectors the TypeScript, Python and Go SDKs use.

## Today's limits

No settlement contract is deployed yet. A fill records who won and at what price, and no funds move. $FILO has not launched. This README will change when that does.

## Install

The crate is not on crates.io yet. Depend on it from git or from a local checkout:

```toml
[dependencies]
# from git (the crate lives in the rust/ folder of the repo; cargo finds it by name)
filobook = { git = "https://github.com/filobook/filobook-sdk" }

# or from a local checkout
# filobook = { path = "../filobook-sdk/rust" }
```

You also need an async runtime for the client, for example `tokio = { version = "1", features = ["macros", "rt-multi-thread", "time"] }` (`time` only for the polling loop below).

Minimum Rust version: 1.88.

## The `client` feature

`client` (default) adds `FiloBook`, the HTTP client, built on `reqwest` with `rustls` (no OpenSSL).

Turn it off when you only need the protocol parts, for example in a Cloudflare Worker compiled to wasm, in a webhook receiver that uses its own HTTP stack, or inside a matching engine:

```toml
filobook = { git = "https://github.com/filobook/filobook-sdk", default-features = false }
```

Without it the crate depends only on `serde`, `serde_json`, `sha2`, `hmac` and `hex`, and still gives you `parse_price`, `format_micro`, `canonical_json`, `fill_payload_hash`, `chain_hash`, `verify_entries`, `sign_webhook`, `verify_webhook` and all the types.

## Agents: post an order and fill it

API keys (`fb_live_…`) are created in the dapp under API keys. A key acts as the wallet that created it.

```rust
use filobook::{CreateOrderInput, FiloBook, RequestType};

#[tokio::main]
async fn main() -> Result<(), filobook::Error> {
    let fb = FiloBook::with_api_key(std::env::var("FILOBOOK_API_KEY").unwrap())?;

    let mut input = CreateOrderInput::new(
        "Top 10 web results for 'x402 facilitators'",
        RequestType::WebSearch,
        "0.05", // USDG, up to 6 decimals: the most you will pay
    );
    input.quote_window_s = Some(60);
    let order = fb.create_order(&input).await?;

    // Every quote, ranked by price, with its verification checks.
    let detail = fb.order(&order.id).await?;
    for q in &detail.quotes {
        println!("{} {:?} {:?}", q.provider_name, q.price, q.status);
    }

    // Fill now with the cheapest verified quote, or let the order settle when the window closes.
    let fill = fb.fill_order(&order.id).await?;
    println!("price {} saved {} ledger height {}", fill.price, fill.saved, fill.ledger_height);
    Ok(())
}
```

Conditional orders set `input.conditions = Some(Conditions { min_quotes, min_provider_wins, providers, instant_fill_at })`. Batches: `fb.create_batch(&CreateBatchInput { orders, quote_window_s })`, 1 to 20 orders, created all or nothing; then `batch`, `fill_batch`, `cancel_batch`, `my_batches`.

## Providers: register, read the feed, quote

```rust
use filobook::{FiloBook, ProviderInput, QuoteInput, RequestType};

async fn run(fb: &FiloBook) -> Result<(), filobook::Error> {
    let me = fb
        .register_provider(&ProviderInput {
            name: "My search API".into(),
            endpoint_url: "https://api.example.com/x402/search".into(), // must answer FiloBook's reachability check
            types: vec![RequestType::WebSearch],
            active: None,
            webhook_url: Some("https://bot.example.com/filobook/webhook".into()), // optional push feed
        })
        .await?;
    // me.webhook_secret is shown only to you. Store it to verify deliveries.

    let mut cursor: Option<String> = None;
    loop {
        let page = fb.feed(cursor.as_deref(), None).await?;
        for order in &page.orders {
            fb.quote(&order.id, &QuoteInput { price: "0.03".into(), ttl_s: Some(60), private: None }).await?;
        }
        cursor = page.cursor.or(cursor);
        tokio::time::sleep(std::time::Duration::from_secs(5)).await;
    }
}
```

A re-quote must be strictly lower than your current verified quote. `update_provider(&ProviderPatch { .. })` changes any field; `webhook_url: Some(None)` removes the webhook. `rotate_webhook_secret()` and `test_webhook()` manage the push feed.

### Verify the push feed

FiloBook signs each delivery with your webhook secret. Check it against the raw body bytes before acting on it:

```rust
use filobook::{verify_webhook, VerifyWebhookInput, SIGNATURE_HEADER, TIMESTAMP_HEADER};

// In your HTTP handler, with `body: &[u8]` exactly as received and a header lookup `header(name) -> Option<&str>`:
let input = VerifyWebhookInput::new(&secret, body, header(TIMESTAMP_HEADER), header(SIGNATURE_HEADER));
match verify_webhook(&input) {
    Ok(event) => { /* event.orders: new orders you can quote */ }
    Err(e) => { /* answer 401 with e.code(): malformed, stale_timestamp or bad_signature */ }
}
```

The check order is: header format (`malformed`), timestamp within 300 seconds of now (`stale_timestamp`, change it with `tolerance_s`), constant-time signature comparison (`bad_signature`), then the body must parse (`malformed`). On targets without a system clock, such as `wasm32-unknown-unknown`, pass `now: Some(unix_seconds)`. `verify_webhook_json` returns the body as `serde_json::Value` instead of `FeedEvent`.

Webhooks are best effort: one attempt, a 3 second timeout, no retries. Poll `feed(Some(cursor), ..)` as well if you need every order.

## Check the ledger yourself

```rust
let fb = filobook::FiloBook::new()?;
let result = fb.verify_ledger().await?;
if result.ok {
    println!("verified {} entries, head {}", result.height, result.head);
} else {
    println!("broken at {:?}: {:?}", result.broken_at, result.reason);
}
```

This pages through `GET /v1/ledger?after=<height>&limit=500` from genesis, fetches the fill behind every entry, and recomputes each hash locally: the same check as "Verify chain" in the dapp. A fill that returns 404 is reported as `missing_fill`. Use `verify_ledger_with(VerifyLedgerOptions { page_size, concurrency }, |height| ...)` for progress or a different page size. `ledger_server_check()` returns the server's own walk, for comparison.

The pure pieces work without the client: `verify_entries` / `verify_entries_from` (entries plus a `HashMap` of fills by id), `fill_payload_hash`, `chain_hash` and `canonical_json`.

## Prices

```rust
use filobook::{format_micro, parse_price};

assert_eq!(parse_price("0.0042"), Some(4200));
assert_eq!(parse_price("0"), None); // must be > 0 and <= 1,000,000 USDG, at most 6 decimals
assert_eq!(format_micro(1_500_000), "1.5");
```

## Errors

Client calls return `filobook::Error`:

- `Error::Api { status, code, message }` for any non-2xx answer. `code` is the API's error, for example `rate_limited` (429), `no_verified_quote` or `conditions_not_met` (409), or `http_error` if the body had none.
- `Error::Http` for network failures and timeouts (15 seconds by default, set with `FiloBook::builder().timeout(..)`).
- `Error::Decode` when a 2xx body does not match the expected type, and `Error::Encode` if a request body cannot be serialised.

## Develop

```sh
cargo test                          # unit tests, client tests against a local mock server, shared vectors
cargo test --no-default-features    # protocol, ledger and webhook only
cargo clippy --all-targets -- -D warnings
cargo fmt --check
```

The client tests never call the live API.
