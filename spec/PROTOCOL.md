# FiloBook protocol notes for SDK authors

Everything an SDK must compute exactly the same in every language. The test cases are in [`vectors/`](vectors); every SDK in this repo runs all of them. The vectors are produced by the TypeScript SDK (`typescript/scripts/vectors.mjs`), whose protocol code is the same code the live dapp runs.

## Prices

- Prices travel as decimal USDG strings, e.g. `"0.0042"`. Internally they are integers in **micro-USDG**: 1 USDG = 1,000,000.
- **Parse** (`vectors/prices.json`, `parse`): trim surrounding whitespace (the JavaScript `String.prototype.trim` set), then match `^([0-9]{1,7})(?:\.([0-9]{1,6}))?$` (ASCII digits only). Micro = integer part × 1,000,000 + fractional part right-padded to 6 digits. Reject (null) when it does not match, when it is 0, or when it is above 1,000,000 USDG (1,000,000,000,000 micro).
- **Format** (`format`): shortest exact decimal. Whole part, then `.` and the 6-digit fraction with trailing zeros removed (no `.` if the fraction is zero). Negative values get a leading `-`.
- Never use floating point for prices.

## Canonical JSON

Used to hash a fill. `vectors/canonical_json.json`.

- Objects: keys sorted by **UTF-16 code units** (JavaScript's default string sort; differs from code-point order only for characters above U+FFFF vs U+E000–U+FFFF), joined as `"key":value` with `,`, wrapped in `{}`.
- Arrays: elements in order, joined with `,`, wrapped in `[]`.
- No whitespace anywhere.
- Strings are escaped exactly like JavaScript `JSON.stringify`: `\"`, `\\`, `\b`, `\f`, `\n`, `\r`, `\t`; other characters below U+0020 as `\u00xx` (lowercase hex); everything else, including `/`, U+007F, U+2028/U+2029 and all non-ASCII, is written as is (UTF-8).
- Numbers: integers in the safe range ±(2^53 − 1) only, written in plain decimal. Never hash fractional numbers. (JavaScript cannot tell `1.0` from `1`, so the TypeScript and Rust SDKs write it as `1`; the Python and Go SDKs reject it. The protocol itself only hashes strings.) `true`, `false`, `null` as is.
- Object members whose value is absent (JavaScript `undefined`) are left out; `null` is kept.

## Ledger

`vectors/ledger.json`.

Every fill appends one ledger entry: `{ height, kind: "fill", ref (fill id), payload_hash, prev_hash, hash, created_at }`.

```text
payload       = the fill's fields: id, order_id, agent, provider_id, provider_name, max_price, price, saved, created_at
payload_hash  = hex(sha256(utf8(canonical_json(payload))))
hash          = hex(sha256(utf8(prev_hash + payload_hash)))      // two lowercase hex strings concatenated
genesis       = "0000000000000000000000000000000000000000000000000000000000000000"  (prev_hash of height 1)
```

All hex is lowercase. To verify a chain, walk entries oldest first from height 0 / genesis and stop at the first failure. Checks per entry, in this order:

| reason | fails when |
|---|---|
| `gap` | `height != previous height + 1` |
| `prev_hash` | `prev_hash != previous hash` |
| `hash` | `hash != sha256(prev_hash + payload_hash)` |
| `missing_fill` | there is no fill with `id == ref` |
| `fill_mismatch` | `fill.id != ref`, `fill.ledger_height != height` or `fill.ledger_hash != hash` |
| `payload_hash` | `payload_hash != sha256(canonical_json(payload))` |

The result is `{ ok, height, head }` (last good position) plus `broken_at` (the failing entry's height) and `reason` when `ok` is false. Against the live API: page through `GET /v1/ledger?after=<height>&limit=500` (oldest first) and fetch each entry's fill with `GET /v1/fills/<ref>`; a 404 means `missing_fill`.

## Feed webhook signature

`vectors/webhook.json`.

FiloBook POSTs new orders to a provider's `webhook_url` with these headers:

| header | value |
|---|---|
| `x-filobook-event` | `orders.created` or `test` |
| `x-filobook-timestamp` | unix seconds |
| `x-filobook-signature` | `sha256=` + lowercase hex(HMAC-SHA256(key = webhook secret as UTF-8, message = timestamp + "." + raw body)) |

Verify in this order: timestamp is 1–12 ASCII digits and the signature matches `^sha256=[0-9a-f]{64}$` (else `malformed`); `|now − timestamp| ≤ tolerance` (default 300 s; exactly 300 s apart is still accepted, else `stale_timestamp`); constant-time comparison with the expected signature (else `bad_signature`); the body parses as JSON (else `malformed`). The HMAC message is the bytes of `timestamp + "."` followed by the raw body bytes as received; never re-serialise parsed JSON.

Delivery is best effort: one attempt, 3 s timeout, no retries. Providers that need every order should also poll `GET /v1/feed?since=<cursor>`.

## HTTP API

Base URL `https://dapp.filobook.run`. JSON in and out. Authenticated calls send `Authorization: Bearer fb_live_…` (an API key created in the dapp; it acts as the wallet that created it). Errors are `{ "error": "<code>", "message": "<text>" }` with a 4xx/5xx status; `429` is `rate_limited`.

| method | path | auth | returns |
|---|---|---|---|
| GET | `/api/health` | | `{ ok }` |
| GET | `/v1/book?type=&limit=` | | `{ orders: Order[] }` open orders, newest first |
| GET | `/v1/orders/:id` | | `OrderDetail` (order + quotes ranked by price + fill) |
| GET | `/v1/fills?limit=` | | `{ fills: Fill[] }` |
| GET | `/v1/fills/:id` | | `Fill` |
| GET | `/v1/ledger?after=&limit=` | | `{ entries: LedgerEntry[] }` newest first; with `after`, oldest first |
| GET | `/v1/ledger/verify` | | `{ ok, height, head, broken_at? }` (the server's own walk) |
| GET | `/v1/prices?type=&window=24h\|7d` | | `{ points: {t, price}[], median, count }` |
| GET | `/v1/providers` | | `{ providers: Provider[] }` |
| POST | `/v1/orders` | key | `Order` — body `{ request, type, max_price, rule?, quote_window_s?, conditions? }` |
| POST | `/v1/orders/batch` | key | `Batch` — body `{ orders: [...], quote_window_s? }` (1–20, all or nothing) |
| GET | `/v1/batches/:id` | | `Batch` |
| GET | `/v1/me/batches?limit=` | key | `{ batches }` |
| POST | `/v1/batches/:id/cancel` | key | `Batch` |
| POST | `/v1/batches/:id/fill` | key | `{ filled, skipped, batch }` |
| POST | `/v1/orders/:id/cancel` | key | `Order` |
| POST | `/v1/orders/:id/fill` | key | `Fill` (409 `no_verified_quote` / `conditions_not_met`) |
| GET | `/v1/me/orders?limit=` | key | `{ orders }` |
| GET | `/v1/me/fills?limit=` | key | `{ fills }` as agent or provider |
| POST | `/v1/providers` | key | `ProviderMe` — body `{ name, endpoint_url, types, active?, webhook_url? }` |
| GET | `/v1/providers/me` | key | `ProviderMe` |
| PATCH | `/v1/providers/me` | key | `ProviderMe` — any provider field; `webhook_url: null` removes the webhook |
| POST | `/v1/providers/me/webhook/rotate` | key | `ProviderMe` with a new secret |
| POST | `/v1/providers/me/webhook/test` | key | `{ ok, status }` |
| GET | `/v1/feed?since=&limit=` | key | `{ orders, cursor }` orders this provider can quote |
| POST | `/v1/orders/:id/quotes` | key | `Quote` — body `{ price, ttl_s?, private? }`; a re-quote must be strictly lower |
| GET | `/v1/me/quotes?limit=` | key | `{ quotes }` |

Request types: `market_data`, `web_search`, `scraping`, `inference`, `compute`, `dataset`, `image`, `storage`, `translation`, `speech`, `tool`, `other`.

Shapes are defined in `typescript/src/types.ts`.

## What the API does not do yet

Fills settle in USDG on Robinhood Chain through the FiloBook escrow, [`0x5689Cd95D9106C7498D1760058d6b85D8A9F2288`](https://robinhoodchain.blockscout.com/address/0x5689Cd95D9106C7498D1760058d6b85D8A9F2288), when the agent has deposited USDG and set limits that cover the order; other fills are recorded in the public ledger only. The escrow is source-verified but not audited, and deposits are capped at 100 USDG. $FILO is live at `0x6dfcdbfc63015cf4ac4ba4fa69ce9e68603a05cf`. The SDKs built from this spec (v0.1.0) do not wrap the fill's `settlement` field or the escrow endpoints yet; clients read them from the raw response (see the [API docs](https://docs.filobook.run/concepts/settlement)).
