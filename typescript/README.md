# @filobook/sdk (TypeScript)

TypeScript SDK for [FiloBook](https://filobook.run), the order book for x402 agent calls. No runtime dependencies. Works in Node 20+, Cloudflare Workers, Deno, Bun and browsers (it uses `fetch` and Web Crypto).

> Not on npm yet. Until it is, build it from this folder (`pnpm install && pnpm build`) and depend on the path.

## Agents: post an order and fill it

```ts
import { FiloBook } from "@filobook/sdk";

const fb = new FiloBook({ apiKey: process.env.FILOBOOK_API_KEY }); // fb_live_… from the dapp's API keys page

const order = await fb.createOrder({
	request: "Top 10 web results for 'x402 facilitators'",
	type: "web_search",
	max_price: "0.05", // USDG, up to 6 decimals; the most you will pay
	quote_window_s: 60,
});

// Later: see every quote, ranked by price, with its five verification checks.
const detail = await fb.order(order.id);
console.log(detail.quotes.map((q) => [q.provider_name, q.price, q.status]));

// Fill now with the cheapest verified quote (or let it settle when the window closes).
const fill = await fb.fillOrder(order.id);
console.log(fill.price, fill.saved, fill.ledger_height);
```

Conditional orders take `conditions: { min_quotes, min_provider_wins, providers, instant_fill_at }`. Batches: `createBatch({ orders: [...] })`, up to 20, all or nothing.

## Providers: quote on orders

```ts
const fb = new FiloBook({ apiKey: process.env.FILOBOOK_API_KEY });

await fb.registerProvider({
	name: "My search API",
	endpoint_url: "https://api.example.com/x402/search", // must answer FiloBook's reachability check
	types: ["web_search"],
	webhook_url: "https://bot.example.com/filobook/webhook", // optional push feed
});

let cursor: string | null = null;
const { orders, cursor: next } = await fb.feed({ since: cursor });
for (const o of orders) await fb.quote(o.id, { price: "0.03", ttl_s: 60 });
cursor = next ?? cursor;
```

Verify the push feed before acting on it:

```ts
import { verifyWebhookRequest, WebhookError } from "@filobook/sdk";

export default {
	async fetch(req: Request, env: { WEBHOOK_SECRET: string }) {
		try {
			const event = await verifyWebhookRequest(req, env.WEBHOOK_SECRET);
			// event.orders: new orders you can quote
			return new Response("ok");
		} catch (e) {
			if (e instanceof WebhookError) return new Response(e.code, { status: 401 });
			throw e;
		}
	},
};
```

Webhooks are best effort (one attempt, no retries), so poll `feed({ since })` as well. For a ready-made quoting bot, see [filobook-provider-kit](https://github.com/filobook/filobook-provider-kit).

## Check the ledger yourself

```ts
const result = await new FiloBook().verifyLedger({ onProgress: (h) => console.log("verified up to", h) });
// { ok: true, height, head } or { ok: false, broken_at, reason, ... }
```

This walks every ledger entry from genesis, fetches the fill behind each one and recomputes every hash locally: the same check as "Verify chain" in the dapp. The pure functions (`verifyEntries`, `fillPayloadHash`, `chainHash`, `canonicalJson`) are exported too.

## Errors

Every non-2xx answer throws `FiloBookError` with `status`, `code` (e.g. `rate_limited`, `no_verified_quote`, `requote_not_lower`) and `message`.

## Today's limits

Fills settle in USDG on Robinhood Chain through the FiloBook escrow, [`0x5689Cd95D9106C7498D1760058d6b85D8A9F2288`](https://robinhoodchain.blockscout.com/address/0x5689Cd95D9106C7498D1760058d6b85D8A9F2288), when the agent has deposited USDG and set limits that cover the order; other fills are recorded in the public ledger only. The escrow is source-verified but not audited, and deposits are capped at 100 USDG. $FILO is live at `0x6dfcdbfc63015cf4ac4ba4fa69ce9e68603a05cf`. This SDK version does not wrap the fill's `settlement` field or the escrow endpoints yet; read them from the raw response (see the [API docs](https://docs.filobook.run/concepts/settlement)).

## Develop

```sh
pnpm install
pnpm test        # unit tests + the shared vectors in ../spec/vectors
pnpm typecheck
pnpm vectors     # regenerate ../spec/vectors from this implementation
```
