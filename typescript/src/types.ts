// API shapes, as returned by https://dapp.filobook.run. Prices are decimal USDG strings ("0.0042").

export const REQUEST_TYPES = [
	"market_data",
	"web_search",
	"scraping",
	"inference",
	"compute",
	"dataset",
	"image",
	"storage",
	"translation",
	"speech",
	"tool",
	"other",
] as const;
export type RequestType = (typeof REQUEST_TYPES)[number];

export type OrderStatus = "open" | "filled" | "expired" | "cancelled";
export type QuoteStatus = "verified" | "failed" | "expired" | "replaced" | "won";
export type CloseReason = "min_quotes" | "no_quote";
export type CheckName = "price_within_max" | "provider_active" | "serves_type" | "quote_not_expired" | "endpoint_reachable";
export type Check = { name: CheckName; ok: boolean; detail: string };

/** Extra rules on top of "cheapest verified quote at or under the max". */
export type Conditions = {
	/** Fill only if at least this many verified quotes compete (2–20); otherwise the order expires. */
	min_quotes?: number;
	/** Only providers with at least this many won fills may quote. */
	min_provider_wins?: number;
	/** Only these provider ids (prv_…) may quote. Up to 10. */
	providers?: string[];
	/** Fill the moment a verified quote at or under this price arrives (other conditions still apply). */
	instant_fill_at?: string;
};

export type Order = {
	id: string;
	agent: string;
	request: string;
	type: RequestType;
	max_price: string;
	rule: string | null;
	status: OrderStatus;
	quote_count: number;
	/** Best verified price this viewer may see. */
	best_quote: string | null;
	/** Live private quotes whose price is hidden from this viewer. */
	private_quotes: number;
	conditions: Conditions | null;
	batch_id: string | null;
	close_reason: CloseReason | null;
	created_at: string;
	closes_at: string;
};

export type Quote = {
	id: string;
	order_id: string;
	provider_id: string;
	provider_name: string;
	/** null while the quote is private and hidden from this viewer. */
	price: string | null;
	private: boolean;
	status: QuoteStatus;
	verification: { checks: Check[] };
	created_at: string;
	expires_at: string;
};

export type Fill = {
	id: string;
	order_id: string;
	agent: string;
	provider_id: string;
	provider_name: string;
	max_price: string;
	price: string;
	saved: string;
	ledger_height: number;
	ledger_hash: string;
	created_at: string;
};

export type LedgerEntry = { height: number; kind: "fill"; ref: string; payload_hash: string; prev_hash: string; hash: string; created_at: string };

export type OrderDetail = Order & { quotes: Quote[]; fill: Fill | null };

export type Provider = { id: string; name: string; address: string; endpoint_url: string; types: RequestType[]; active: boolean; quotes_won: number; created_at: string };

/** The provider's own view. The webhook secret is only ever shown to its owner. */
export type ProviderMe = Provider & { webhook_url: string | null; webhook_secret: string | null; webhook_status: string | null; webhook_at: string | null };

export type BatchSummary = { orders: number; open: number; filled: number; expired: number; cancelled: number; spent: string; saved: string };
export type Batch = { id: string; agent: string; created_at: string; summary: BatchSummary; orders: Order[] };
export type BatchListItem = { id: string; created_at: string; summary: BatchSummary };

/** Body of a feed webhook (POST to the provider's webhook_url). */
export type FeedEvent = { event: "orders.created" | "test"; sent_at: string; orders: Order[] };

export type PricePoint = { t: string; price: string };
export type Prices = { points: PricePoint[]; median: string | null; count: number };

export type LedgerServerCheck = { ok: boolean; height: number; head: string; broken_at?: number };

// ---------- inputs ----------

export type OrderInput = {
	request: string;
	type: RequestType;
	/** Most you will pay, as a decimal USDG string with up to 6 decimals. */
	max_price: string;
	rule?: string | null;
	conditions?: Conditions | null;
};
export type CreateOrderInput = OrderInput & {
	/** How long quotes are accepted, in seconds (10–600, default 60). */
	quote_window_s?: number;
};
export type CreateBatchInput = { orders: OrderInput[]; quote_window_s?: number };

export type QuoteInput = {
	price: string;
	/** Quote lifetime in seconds (10–3600, default 60). */
	ttl_s?: number;
	/** Hide the price from everyone but the agent until the order closes. */
	private?: boolean;
};

export type ProviderInput = { name: string; endpoint_url: string; types: RequestType[]; active?: boolean; webhook_url?: string | null };
export type ProviderPatch = Partial<ProviderInput>;
