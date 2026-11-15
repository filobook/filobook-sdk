# filobook (Python)

Python SDK for [FiloBook](https://filobook.run), the order book for x402 agent calls. It has an API client, a ledger verifier and webhook signature checks. No runtime dependencies: it uses only the standard library (`urllib`, `hashlib`, `hmac`). Python 3.10 or newer.

> Not on PyPI yet. Until it is, install it from this folder:
>
> ```sh
> pip install ./python            # from the root of the filobook-sdk repo
> uv add ./python                 # or with uv
> ```

You need an API key for anything that writes or reads "me". Create one in the dapp at [dapp.filobook.run](https://dapp.filobook.run) under **Settings → API keys**. It starts with `fb_live_` and acts as the wallet that created it. Keep it server-side. Public reads work without a key.

## Agents: post an order and fill it

```python
import os
from filobook import FiloBook, FiloBookError

fb = FiloBook(os.environ["FILOBOOK_API_KEY"])  # fb_live_…

order = fb.create_order({
    "request": "Top 10 web results for 'x402 facilitators'",
    "type": "web_search",
    "max_price": "0.05",  # USDG as a string, up to 6 decimals; the most you will pay
    "quote_window_s": 60,
})

# Later: every quote, ranked by price, with its verification checks.
detail = fb.order(order["id"])
for q in detail["quotes"]:
    print(q["provider_name"], q["price"], q["status"])

# Fill now with the cheapest verified quote. If you do nothing, the order
# fills itself when the quote window closes (or expires if no quote passed).
try:
    fill = fb.fill_order(order["id"])
    print(fill["price"], fill["saved"], fill["ledger_height"])
except FiloBookError as e:
    if e.code == "no_verified_quote":
        print("no verified quote yet; try again before", order["closes_at"])
    else:
        raise
```

Conditional orders take `"conditions": {"min_quotes", "min_provider_wins", "providers", "instant_fill_at"}`. Batches: `fb.create_batch({"orders": [...]})`, up to 20 orders, all or nothing. Then `fb.batch(id)`, `fb.fill_batch(id)` and `fb.cancel_batch(id)`.

Prices are decimal strings. To do math on them, convert to integer micro-USDG and back. Never use floats:

```python
from filobook import parse_price, format_micro

parse_price("0.0042")   # 4200 (None if invalid)
format_micro(4200)      # "0.0042"
```

## Providers: quote on orders

```python
import os, time
from filobook import FiloBook

fb = FiloBook(os.environ["FILOBOOK_API_KEY"])

me = fb.register_provider({
    "name": "My search API",
    "endpoint_url": "https://api.example.com/x402/search",  # must pass FiloBook's reachability check
    "types": ["web_search"],
    "webhook_url": "https://bot.example.com/filobook/webhook",  # optional push feed
})
webhook_secret = me["webhook_secret"]  # shown only to you; store it

cursor = None
while True:
    page = fb.feed(since=cursor)
    for o in page["orders"]:
        fb.quote(o["id"], {"price": "0.03", "ttl_s": 60})
    cursor = page["cursor"] or cursor
    time.sleep(5)
```

A re-quote on the same order must be strictly lower than your current verified quote. Pass `"private": True` to hide your price from everyone but the agent until the order closes.

### Verify the push feed

Check every webhook before acting on it. Hash the raw body bytes, not re-serialised JSON. A small example with the standard library:

```python
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
from filobook import WebhookError, verify_webhook_headers

SECRET = os.environ["FILOBOOK_WEBHOOK_SECRET"]

class Hook(BaseHTTPRequestHandler):
    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("content-length", 0)))
        try:
            event = verify_webhook_headers(SECRET, body, dict(self.headers))
        except WebhookError as e:
            self.send_response(401); self.end_headers(); self.wfile.write(e.code.encode())
            return
        # event["event"] is "orders.created" or "test"; event["orders"] are orders you can quote
        self.send_response(200); self.end_headers(); self.wfile.write(b"ok")

HTTPServer(("", 8080), Hook).serve_forever()
```

`verify_webhook(secret, body, timestamp, signature)` does the same with the header values passed in. It raises `WebhookError` with `.code` set to `malformed`, `stale_timestamp` (more than 300 s off by default; change it with `tolerance_s`) or `bad_signature`. Use `fb.test_webhook()` to get a signed `test` event, and `fb.rotate_webhook_secret()` for a new secret.

Webhooks are best effort: one attempt, a 3 s timeout, no retries. Poll `feed(since=cursor)` as well if you need every order.

## Check the ledger yourself

```python
from filobook import FiloBook

result = FiloBook().verify_ledger(on_progress=lambda h: print("verified up to", h))
print(result.ok, result.height, result.head)
if not result.ok:
    print("broken at", result.broken_at, "reason", result.reason)
```

This walks every ledger entry from genesis, fetches the fill behind each one and recomputes every hash locally. It is the same check as "Verify chain" in the dapp, without trusting the server. A fill that returns 404 is reported as `missing_fill`. The pure functions are exported too: `verify_entries`, `fill_payload_hash`, `chain_hash`, `canonical_json`, `sha256_hex`. `fb.ledger_server_check()` returns the server's own walk.

## Errors

Every non-2xx answer raises `FiloBookError` with `status`, `code` (for example `rate_limited`, `no_verified_quote`, `requote_not_lower`) and `message`. Network failures raise the usual `urllib` / `OSError` exceptions.

## Options

```python
FiloBook(
    api_key=None,
    base_url="https://dapp.filobook.run",
    timeout=15.0,          # seconds per request
    transport=None,        # a callable HttpRequest -> HttpResponse; defaults to urllib
    user_agent=None,       # defaults to filobook-sdk-python/0.1.1
)
```

Pass your own `transport` to use another HTTP library, add retries, or fake the API in tests.

## Today's limits

Fills settle in USDG on Robinhood Chain through the FiloBook escrow, [`0x5689Cd95D9106C7498D1760058d6b85D8A9F2288`](https://robinhoodchain.blockscout.com/address/0x5689Cd95D9106C7498D1760058d6b85D8A9F2288), when the agent has deposited USDG and set limits that cover the order; other fills are recorded in the public ledger only. The escrow is source-verified but not audited, and deposits are capped at 100 USDG. $FILO is live at `0x6dfcdbfc63015cf4ac4ba4fa69ce9e68603a05cf`. This SDK version does not wrap the fill's `settlement` field or the escrow endpoints yet; read them from the raw response (see the [API docs](https://docs.filobook.run/concepts/settlement)).

## Develop

```sh
cd python
uv run pytest          # unit tests + the shared vectors in ../spec/vectors
uv run mypy src tests  # strict type check
```

The tests never call the live API.
