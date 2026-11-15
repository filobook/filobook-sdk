//! API shapes, as returned by `https://dapp.filobook.run`. Prices are decimal USDG strings (`"0.0042"`);
//! use [`crate::parse_price`] to turn them into micro-USDG integers. Timestamps are ISO 8601 strings.
//!
//! Status-like enums carry an `Unknown` variant so a value added to the API later does not break
//! deserialisation.

use serde::{Deserialize, Serialize};

/// What an order asks for. Serialised in snake_case (`"web_search"`).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[allow(missing_docs)]
pub enum RequestType {
    MarketData,
    WebSearch,
    Scraping,
    Inference,
    Compute,
    Dataset,
    Image,
    Storage,
    Translation,
    Speech,
    Tool,
    Other,
}

impl RequestType {
    /// Every request type, in the API's order.
    pub const ALL: [RequestType; 12] = [
        Self::MarketData,
        Self::WebSearch,
        Self::Scraping,
        Self::Inference,
        Self::Compute,
        Self::Dataset,
        Self::Image,
        Self::Storage,
        Self::Translation,
        Self::Speech,
        Self::Tool,
        Self::Other,
    ];

    /// The wire name, e.g. `"web_search"`.
    pub fn as_str(self) -> &'static str {
        match self {
            Self::MarketData => "market_data",
            Self::WebSearch => "web_search",
            Self::Scraping => "scraping",
            Self::Inference => "inference",
            Self::Compute => "compute",
            Self::Dataset => "dataset",
            Self::Image => "image",
            Self::Storage => "storage",
            Self::Translation => "translation",
            Self::Speech => "speech",
            Self::Tool => "tool",
            Self::Other => "other",
        }
    }
}

impl std::fmt::Display for RequestType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

impl std::str::FromStr for RequestType {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Self::ALL
            .into_iter()
            .find(|t| t.as_str() == s)
            .ok_or_else(|| format!("unknown request type: {s}"))
    }
}

/// Lifecycle of an order.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Default, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum OrderStatus {
    Open,
    Filled,
    Expired,
    Cancelled,
    /// A status this SDK version does not know, or a payload without one.
    #[default]
    #[serde(other)]
    Unknown,
}

/// Lifecycle of a quote.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum QuoteStatus {
    Verified,
    Failed,
    Expired,
    Replaced,
    Won,
    #[serde(other)]
    Unknown,
}

/// Why an order closed without a fill.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum CloseReason {
    MinQuotes,
    NoQuote,
    #[serde(other)]
    Unknown,
}

/// One of the verification checks run on every quote.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum CheckName {
    PriceWithinMax,
    ProviderActive,
    ServesType,
    QuoteNotExpired,
    EndpointReachable,
    #[serde(other)]
    Unknown,
}

/// A verification check and its outcome.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Check {
    pub name: CheckName,
    pub ok: bool,
    pub detail: String,
}

/// Extra rules on top of "cheapest verified quote at or under the max".
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
pub struct Conditions {
    /// Fill only if at least this many verified quotes compete (2–20); otherwise the order expires.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub min_quotes: Option<u32>,
    /// Only providers with at least this many won fills may quote.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub min_provider_wins: Option<u32>,
    /// Only these provider ids (`prv_…`) may quote. Up to 10.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub providers: Option<Vec<String>>,
    /// Fill the moment a verified quote at or under this price arrives (other conditions still apply).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub instant_fill_at: Option<String>,
}

/// An order on the book.
///
/// The API always sends every field; `id`, `request`, `type` and `max_price` are the only ones required
/// when deserialising, so partial payloads (e.g. hand-written webhook tests) still parse.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Order {
    #[allow(missing_docs)]
    pub id: String,
    /// The agent's wallet address.
    #[serde(default)]
    pub agent: String,
    #[allow(missing_docs)]
    pub request: String,
    /// What the order asks for.
    #[serde(rename = "type")]
    pub request_type: RequestType,
    /// Most the agent will pay, decimal USDG.
    pub max_price: String,
    #[allow(missing_docs)]
    #[serde(default)]
    pub rule: Option<String>,
    #[allow(missing_docs)]
    #[serde(default)]
    pub status: OrderStatus,
    #[allow(missing_docs)]
    #[serde(default)]
    pub quote_count: u32,
    /// Best verified price this viewer may see.
    #[serde(default)]
    pub best_quote: Option<String>,
    /// Live private quotes whose price is hidden from this viewer.
    #[serde(default)]
    pub private_quotes: u32,
    #[allow(missing_docs)]
    #[serde(default)]
    pub conditions: Option<Conditions>,
    #[allow(missing_docs)]
    #[serde(default)]
    pub batch_id: Option<String>,
    #[allow(missing_docs)]
    #[serde(default)]
    pub close_reason: Option<CloseReason>,
    #[allow(missing_docs)]
    #[serde(default)]
    pub created_at: String,
    /// When the quote window closes.
    #[serde(default)]
    pub closes_at: String,
}

/// The checks run on a quote.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Verification {
    pub checks: Vec<Check>,
}

/// A provider's price for an order.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Quote {
    pub id: String,
    pub order_id: String,
    pub provider_id: String,
    pub provider_name: String,
    /// `None` while the quote is private and hidden from this viewer.
    pub price: Option<String>,
    pub private: bool,
    pub status: QuoteStatus,
    pub verification: Verification,
    pub created_at: String,
    pub expires_at: String,
}

/// A filled order: who won and at what price. Funded fills also settle in USDG on Robinhood Chain (see the `settlement` field in the raw response).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Fill {
    pub id: String,
    pub order_id: String,
    pub agent: String,
    pub provider_id: String,
    pub provider_name: String,
    pub max_price: String,
    pub price: String,
    pub saved: String,
    pub ledger_height: u64,
    pub ledger_hash: String,
    pub created_at: String,
}

/// Kind of ledger entry. Only fills today.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum LedgerKind {
    Fill,
    #[serde(other)]
    Unknown,
}

/// One link of the fill ledger hash chain.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct LedgerEntry {
    pub height: u64,
    pub kind: LedgerKind,
    /// The fill id.
    #[serde(rename = "ref")]
    pub fill_ref: String,
    pub payload_hash: String,
    pub prev_hash: String,
    pub hash: String,
    pub created_at: String,
}

/// An order with every quote (ranked by price) and its fill, if any.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct OrderDetail {
    #[serde(flatten)]
    pub order: Order,
    pub quotes: Vec<Quote>,
    pub fill: Option<Fill>,
}

/// A registered provider (public view).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Provider {
    pub id: String,
    pub name: String,
    pub address: String,
    pub endpoint_url: String,
    pub types: Vec<RequestType>,
    pub active: bool,
    pub quotes_won: u64,
    pub created_at: String,
}

/// The provider's own view. The webhook secret is only ever shown to its owner.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct ProviderMe {
    #[serde(flatten)]
    pub provider: Provider,
    pub webhook_url: Option<String>,
    pub webhook_secret: Option<String>,
    pub webhook_status: Option<String>,
    pub webhook_at: Option<String>,
}

/// Counts and totals for a batch.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct BatchSummary {
    pub orders: u32,
    pub open: u32,
    pub filled: u32,
    pub expired: u32,
    pub cancelled: u32,
    pub spent: String,
    pub saved: String,
}

/// A batch of orders created together.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Batch {
    pub id: String,
    pub agent: String,
    pub created_at: String,
    pub summary: BatchSummary,
    pub orders: Vec<Order>,
}

/// A batch in a list, without its orders.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct BatchListItem {
    pub id: String,
    pub created_at: String,
    pub summary: BatchSummary,
}

/// Result of filling a batch.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct BatchFillResult {
    pub filled: u32,
    pub skipped: u32,
    pub batch: Batch,
}

/// Event type of a feed webhook.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[non_exhaustive]
#[allow(missing_docs)]
pub enum FeedEventKind {
    #[serde(rename = "orders.created")]
    OrdersCreated,
    #[serde(rename = "test")]
    Test,
    #[serde(other)]
    Unknown,
}

/// Body of a feed webhook (POST to the provider's `webhook_url`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct FeedEvent {
    pub event: FeedEventKind,
    pub sent_at: String,
    pub orders: Vec<Order>,
}

/// One page of the provider feed.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FeedPage {
    /// Open orders this provider can quote.
    pub orders: Vec<Order>,
    /// Pass as `since` on the next call to get only newer orders.
    pub cursor: Option<String>,
}

/// A filled price at a point in time.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct PricePoint {
    pub t: String,
    pub price: String,
}

/// Filled prices over a window, with the median.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Prices {
    pub points: Vec<PricePoint>,
    pub median: Option<String>,
    pub count: u64,
}

/// Window for [`Prices`].
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub enum PriceWindow {
    /// The last 24 hours.
    #[serde(rename = "24h")]
    Last24h,
    /// The last 7 days.
    #[serde(rename = "7d")]
    Last7d,
}

impl PriceWindow {
    /// The wire name (`"24h"` or `"7d"`).
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Last24h => "24h",
            Self::Last7d => "7d",
        }
    }
}

/// The server's own walk of the ledger.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct LedgerServerCheck {
    pub ok: bool,
    pub height: u64,
    pub head: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub broken_at: Option<u64>,
}

/// `GET /api/health`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct Health {
    pub ok: bool,
}

/// Result of sending a test event to the provider's webhook.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct WebhookTestResult {
    pub ok: bool,
    pub status: String,
}

// ---------- inputs ----------

/// One order in a [`CreateBatchInput`].
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OrderInput {
    #[allow(missing_docs)]
    pub request: String,
    #[allow(missing_docs)]
    #[serde(rename = "type")]
    pub request_type: RequestType,
    /// Most you will pay, as a decimal USDG string with up to 6 decimals.
    pub max_price: String,
    #[allow(missing_docs)]
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rule: Option<String>,
    #[allow(missing_docs)]
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub conditions: Option<Conditions>,
}

impl OrderInput {
    /// An order with no rule and no conditions.
    pub fn new(
        request: impl Into<String>,
        request_type: RequestType,
        max_price: impl Into<String>,
    ) -> Self {
        Self {
            request: request.into(),
            request_type,
            max_price: max_price.into(),
            rule: None,
            conditions: None,
        }
    }
}

/// Body of `POST /v1/orders`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct CreateOrderInput {
    #[allow(missing_docs)]
    pub request: String,
    #[allow(missing_docs)]
    #[serde(rename = "type")]
    pub request_type: RequestType,
    /// Most you will pay, as a decimal USDG string with up to 6 decimals.
    pub max_price: String,
    #[allow(missing_docs)]
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub rule: Option<String>,
    /// How long quotes are accepted, in seconds (10–600, default 60).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub quote_window_s: Option<u32>,
    #[allow(missing_docs)]
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub conditions: Option<Conditions>,
}

impl CreateOrderInput {
    /// An order with the default quote window, no rule and no conditions.
    pub fn new(
        request: impl Into<String>,
        request_type: RequestType,
        max_price: impl Into<String>,
    ) -> Self {
        Self {
            request: request.into(),
            request_type,
            max_price: max_price.into(),
            rule: None,
            quote_window_s: None,
            conditions: None,
        }
    }
}

/// Body of `POST /v1/orders/batch`: 1–20 orders, created all or nothing.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct CreateBatchInput {
    pub orders: Vec<OrderInput>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub quote_window_s: Option<u32>,
}

/// Body of `POST /v1/orders/:id/quotes`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct QuoteInput {
    /// Decimal USDG. A re-quote must be strictly lower than your current verified quote.
    pub price: String,
    /// Quote lifetime in seconds (10–3600, default 60).
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub ttl_s: Option<u32>,
    /// Hide the price from everyone but the agent until the order closes.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub private: Option<bool>,
}

impl QuoteInput {
    /// A public quote with the default lifetime.
    pub fn new(price: impl Into<String>) -> Self {
        Self {
            price: price.into(),
            ttl_s: None,
            private: None,
        }
    }
}

/// Body of `POST /v1/providers`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct ProviderInput {
    pub name: String,
    /// Your x402 endpoint. It must answer FiloBook's reachability check.
    pub endpoint_url: String,
    pub types: Vec<RequestType>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub active: Option<bool>,
    /// Optional push feed URL.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub webhook_url: Option<String>,
}

/// Body of `PATCH /v1/providers/me`. Only the fields that are `Some` are sent.
///
/// `webhook_url` has three states: `None` leaves it unchanged, `Some(None)` sends `null` (removes the
/// webhook and its secret), `Some(Some(url))` sets it.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[allow(missing_docs)]
pub struct ProviderPatch {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub name: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub endpoint_url: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub types: Option<Vec<RequestType>>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub active: Option<bool>,
    #[serde(
        default,
        skip_serializing_if = "Option::is_none",
        with = "double_option"
    )]
    pub webhook_url: Option<Option<String>>,
}

/// Keeps `Some(None)` distinct from `None` when deserialising (`null` vs a missing field).
mod double_option {
    use serde::{Deserialize, Deserializer, Serialize, Serializer};

    pub fn serialize<S: Serializer>(v: &Option<Option<String>>, s: S) -> Result<S::Ok, S::Error> {
        v.serialize(s)
    }

    pub fn deserialize<'de, D: Deserializer<'de>>(
        d: D,
    ) -> Result<Option<Option<String>>, D::Error> {
        Option::<String>::deserialize(d).map(Some)
    }
}
