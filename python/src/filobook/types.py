"""API shapes, as returned by https://dapp.filobook.run. Prices are decimal USDG strings ("0.0042")."""

from __future__ import annotations

from typing import Literal, TypedDict

RequestType = Literal[
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
]

REQUEST_TYPES: tuple[RequestType, ...] = (
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
)

OrderStatus = Literal["open", "filled", "expired", "cancelled"]
QuoteStatus = Literal["verified", "failed", "expired", "replaced", "won"]
CloseReason = Literal["min_quotes", "no_quote"]
CheckName = Literal["price_within_max", "provider_active", "serves_type", "quote_not_expired", "endpoint_reachable"]
PriceWindow = Literal["24h", "7d"]


class Check(TypedDict):
    name: CheckName
    ok: bool
    detail: str


class Verification(TypedDict):
    checks: list[Check]


class Conditions(TypedDict, total=False):
    """Extra rules on top of "cheapest verified quote at or under the max"."""

    min_quotes: int
    """Fill only if at least this many verified quotes compete (2-20); otherwise the order expires."""
    min_provider_wins: int
    """Only providers with at least this many won fills may quote."""
    providers: list[str]
    """Only these provider ids (prv_...) may quote. Up to 10."""
    instant_fill_at: str
    """Fill the moment a verified quote at or under this price arrives (other conditions still apply)."""


class Order(TypedDict):
    id: str
    agent: str
    request: str
    type: RequestType
    max_price: str
    rule: str | None
    status: OrderStatus
    quote_count: int
    best_quote: str | None
    """Best verified price this viewer may see."""
    private_quotes: int
    """Live private quotes whose price is hidden from this viewer."""
    conditions: Conditions | None
    batch_id: str | None
    close_reason: CloseReason | None
    created_at: str
    closes_at: str


class Quote(TypedDict):
    id: str
    order_id: str
    provider_id: str
    provider_name: str
    price: str | None
    """None while the quote is private and hidden from this viewer."""
    private: bool
    status: QuoteStatus
    verification: Verification
    created_at: str
    expires_at: str


class Fill(TypedDict):
    id: str
    order_id: str
    agent: str
    provider_id: str
    provider_name: str
    max_price: str
    price: str
    saved: str
    ledger_height: int
    ledger_hash: str
    created_at: str


class LedgerEntry(TypedDict):
    height: int
    kind: Literal["fill"]
    ref: str
    payload_hash: str
    prev_hash: str
    hash: str
    created_at: str


class OrderDetail(Order):
    quotes: list[Quote]
    fill: Fill | None


class Provider(TypedDict):
    id: str
    name: str
    address: str
    endpoint_url: str
    types: list[RequestType]
    active: bool
    quotes_won: int
    created_at: str


class ProviderMe(Provider):
    """The provider's own view. The webhook secret is only ever shown to its owner."""

    webhook_url: str | None
    webhook_secret: str | None
    webhook_status: str | None
    webhook_at: str | None


class BatchSummary(TypedDict):
    orders: int
    open: int
    filled: int
    expired: int
    cancelled: int
    spent: str
    saved: str


class Batch(TypedDict):
    id: str
    agent: str
    created_at: str
    summary: BatchSummary
    orders: list[Order]


class BatchListItem(TypedDict):
    id: str
    created_at: str
    summary: BatchSummary


class BatchFillResult(TypedDict):
    filled: int
    skipped: int
    batch: Batch


class FeedEvent(TypedDict):
    """Body of a feed webhook (POST to the provider's webhook_url)."""

    event: Literal["orders.created", "test"]
    sent_at: str
    orders: list[Order]


class FeedPage(TypedDict):
    orders: list[Order]
    cursor: str | None


class PricePoint(TypedDict):
    t: str
    price: str


class Prices(TypedDict):
    points: list[PricePoint]
    median: str | None
    count: int


class Health(TypedDict):
    ok: bool


class WebhookTestResult(TypedDict):
    ok: bool
    status: str


class _LedgerServerCheckBase(TypedDict):
    ok: bool
    height: int
    head: str


class LedgerServerCheck(_LedgerServerCheckBase, total=False):
    broken_at: int


# ---------- inputs ----------


class _OrderInputBase(TypedDict):
    request: str
    type: RequestType
    max_price: str
    """Most you will pay, as a decimal USDG string with up to 6 decimals."""


class OrderInput(_OrderInputBase, total=False):
    rule: str | None
    conditions: Conditions | None


class CreateOrderInput(OrderInput, total=False):
    quote_window_s: int
    """How long quotes are accepted, in seconds (10-600, default 60)."""


class _CreateBatchInputBase(TypedDict):
    orders: list[OrderInput]


class CreateBatchInput(_CreateBatchInputBase, total=False):
    quote_window_s: int


class _QuoteInputBase(TypedDict):
    price: str


class QuoteInput(_QuoteInputBase, total=False):
    ttl_s: int
    """Quote lifetime in seconds (10-3600, default 60)."""
    private: bool
    """Hide the price from everyone but the agent until the order closes."""


class _ProviderInputBase(TypedDict):
    name: str
    endpoint_url: str
    types: list[RequestType]


class ProviderInput(_ProviderInputBase, total=False):
    active: bool
    webhook_url: str | None


class ProviderPatch(TypedDict, total=False):
    """Any provider field. webhook_url: None removes the webhook and its secret."""

    name: str
    endpoint_url: str
    types: list[RequestType]
    active: bool
    webhook_url: str | None
