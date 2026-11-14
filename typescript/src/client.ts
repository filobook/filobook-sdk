import { FiloBookError } from "./errors";
import { GENESIS, type ChainResult, verifyEntries } from "./ledger";
import type {
	Batch,
	BatchListItem,
	CreateBatchInput,
	CreateOrderInput,
	Fill,
	LedgerEntry,
	LedgerServerCheck,
	Order,
	OrderDetail,
	Prices,
	Provider,
	ProviderInput,
	ProviderMe,
	ProviderPatch,
	Quote,
	QuoteInput,
	RequestType,
} from "./types";

export const DEFAULT_BASE_URL = "https://dapp.filobook.run";
export const VERSION = "0.1.0";

export type ClientOptions = {
	/** An API key (fb_live_…), created in the dapp under API keys. Needed for anything that posts or reads "me". */
	apiKey?: string;
	baseUrl?: string;
	/** Per-request timeout in ms (default 15000). */
	timeoutMs?: number;
	fetch?: typeof fetch;
	userAgent?: string;
};

type Query = Record<string, string | number | undefined | null>;

export class FiloBook {
	readonly baseUrl: string;
	private readonly apiKey?: string;
	private readonly timeoutMs: number;
	private readonly fetcher: typeof fetch;
	private readonly userAgent: string;

	constructor(opts: ClientOptions = {}) {
		this.baseUrl = (opts.baseUrl ?? DEFAULT_BASE_URL).replace(/\/+$/, "");
		this.apiKey = opts.apiKey;
		this.timeoutMs = opts.timeoutMs ?? 15_000;
		this.fetcher = opts.fetch ?? globalThis.fetch.bind(globalThis);
		this.userAgent = opts.userAgent ?? `filobook-sdk-ts/${VERSION}`;
	}

	private async request<T>(method: string, path: string, opts: { query?: Query; body?: unknown } = {}): Promise<T> {
		const url = new URL(this.baseUrl + path);
		for (const [k, v] of Object.entries(opts.query ?? {})) if (v !== undefined && v !== null) url.searchParams.set(k, String(v));
		const headers: Record<string, string> = { accept: "application/json", "user-agent": this.userAgent };
		if (this.apiKey) headers.authorization = `Bearer ${this.apiKey}`;
		if (opts.body !== undefined) headers["content-type"] = "application/json";
		const res = await this.fetcher(url, {
			method,
			headers,
			body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
			signal: AbortSignal.timeout(this.timeoutMs),
		});
		const text = await res.text();
		let data: unknown = null;
		try {
			data = text ? JSON.parse(text) : null;
		} catch {
			/* not JSON */
		}
		if (!res.ok) {
			const err = (data ?? {}) as { error?: string; message?: string };
			throw new FiloBookError(res.status, err.error ?? "http_error", err.message ?? `HTTP ${res.status}`);
		}
		return data as T;
	}

	// ---------- public reads ----------

	health() {
		return this.request<{ ok: boolean }>("GET", "/api/health");
	}
	/** Open orders, newest first. */
	async book(opts: { type?: RequestType; limit?: number } = {}) {
		return (await this.request<{ orders: Order[] }>("GET", "/v1/book", { query: opts })).orders;
	}
	/** One order with every quote (ranked by price) and its fill, if any. */
	order(id: string) {
		return this.request<OrderDetail>("GET", `/v1/orders/${encodeURIComponent(id)}`);
	}
	async fills(opts: { limit?: number } = {}) {
		return (await this.request<{ fills: Fill[] }>("GET", "/v1/fills", { query: opts })).fills;
	}
	fill(id: string) {
		return this.request<Fill>("GET", `/v1/fills/${encodeURIComponent(id)}`);
	}
	/** Newest first; with `after` (a height), oldest first from that height, for walking the chain. */
	async ledger(opts: { after?: number; limit?: number } = {}) {
		return (await this.request<{ entries: LedgerEntry[] }>("GET", "/v1/ledger", { query: opts })).entries;
	}
	/** The server's own walk of the chain. verifyLedger() does the same walk here, without trusting the server. */
	ledgerServerCheck() {
		return this.request<LedgerServerCheck>("GET", "/v1/ledger/verify");
	}
	/** Filled prices over a window, with the median. */
	prices(opts: { type?: RequestType; window?: "24h" | "7d" } = {}) {
		return this.request<Prices>("GET", "/v1/prices", { query: opts });
	}
	async providers() {
		return (await this.request<{ providers: Provider[] }>("GET", "/v1/providers")).providers;
	}

	// ---------- agents ----------

	createOrder(input: CreateOrderInput) {
		return this.request<Order>("POST", "/v1/orders", { body: input });
	}
	/** Up to 20 orders at once; all are validated first, and either every order is created or none. */
	createBatch(input: CreateBatchInput) {
		return this.request<Batch>("POST", "/v1/orders/batch", { body: input });
	}
	batch(id: string) {
		return this.request<Batch>("GET", `/v1/batches/${encodeURIComponent(id)}`);
	}
	async myBatches(opts: { limit?: number } = {}) {
		return (await this.request<{ batches: BatchListItem[] }>("GET", "/v1/me/batches", { query: opts })).batches;
	}
	cancelBatch(id: string) {
		return this.request<Batch>("POST", `/v1/batches/${encodeURIComponent(id)}/cancel`);
	}
	/** Fill every open order in the batch with its cheapest verified quote. */
	fillBatch(id: string) {
		return this.request<{ filled: number; skipped: number; batch: Batch }>("POST", `/v1/batches/${encodeURIComponent(id)}/fill`);
	}
	cancelOrder(id: string) {
		return this.request<Order>("POST", `/v1/orders/${encodeURIComponent(id)}/cancel`);
	}
	/** Fill now with the cheapest verified quote at or under the max. Throws no_verified_quote / conditions_not_met. */
	fillOrder(id: string) {
		return this.request<Fill>("POST", `/v1/orders/${encodeURIComponent(id)}/fill`);
	}
	async myOrders(opts: { limit?: number } = {}) {
		return (await this.request<{ orders: Order[] }>("GET", "/v1/me/orders", { query: opts })).orders;
	}
	/** Fills where you were the agent or the provider. */
	async myFills(opts: { limit?: number } = {}) {
		return (await this.request<{ fills: Fill[] }>("GET", "/v1/me/fills", { query: opts })).fills;
	}

	// ---------- providers ----------

	registerProvider(input: ProviderInput) {
		return this.request<ProviderMe>("POST", "/v1/providers", { body: input });
	}
	myProvider() {
		return this.request<ProviderMe>("GET", "/v1/providers/me");
	}
	/** Change any field. webhook_url: null removes the webhook and its secret. */
	updateProvider(patch: ProviderPatch) {
		return this.request<ProviderMe>("PATCH", "/v1/providers/me", { body: patch });
	}
	rotateWebhookSecret() {
		return this.request<ProviderMe>("POST", "/v1/providers/me/webhook/rotate");
	}
	/** Send a signed `test` event to your webhook now. */
	testWebhook() {
		return this.request<{ ok: boolean; status: string }>("POST", "/v1/providers/me/webhook/test");
	}
	/**
	 * Open orders you can quote. With `since` (the cursor from the previous call), only newer orders, oldest
	 * first; use it to poll alongside the webhook, which is best effort.
	 */
	feed(opts: { since?: string | null; limit?: number } = {}) {
		return this.request<{ orders: Order[]; cursor: string | null }>("GET", "/v1/feed", { query: opts });
	}
	/** Quote on an order. A re-quote must be strictly lower than your current verified quote. */
	quote(orderId: string, input: QuoteInput) {
		return this.request<Quote>("POST", `/v1/orders/${encodeURIComponent(orderId)}/quotes`, { body: input });
	}
	async myQuotes(opts: { limit?: number } = {}) {
		return (await this.request<{ quotes: Quote[] }>("GET", "/v1/me/quotes", { query: opts })).quotes;
	}

	// ---------- ledger ----------

	/**
	 * Walk the whole ledger from genesis and recompute every hash here, the same check as "Verify chain" in the
	 * dapp. Fetches `pageSize` entries at a time plus the fill behind each one.
	 */
	async verifyLedger(opts: { pageSize?: number; onProgress?: (height: number) => void } = {}): Promise<ChainResult> {
		const pageSize = opts.pageSize ?? 500;
		let pos = GENESIS;
		for (;;) {
			const entries = await this.ledger({ after: pos.height, limit: pageSize });
			if (!entries.length) return { ok: true, ...pos };
			const fills: Record<string, Fill | null> = {};
			await Promise.all(
				entries.map(async (e) => {
					fills[e.ref] = await this.fill(e.ref).catch((err) => {
						if (err instanceof FiloBookError && err.status === 404) return null;
						throw err;
					});
				}),
			);
			const result = await verifyEntries(entries, fills, pos);
			if (!result.ok) return result;
			pos = { height: result.height, head: result.head };
			opts.onProgress?.(pos.height);
		}
	}
}
