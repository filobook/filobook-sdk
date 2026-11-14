//! Async HTTP client for the FiloBook API (cargo feature `client`).

use std::collections::{BTreeSet, HashMap};
use std::fmt;
use std::time::Duration;

use futures_util::stream::{self, StreamExt, TryStreamExt};
use reqwest::{Method, StatusCode};
use serde::de::DeserializeOwned;
use serde::Deserialize;

use crate::ledger::{verify_entries_from, ChainPosition, ChainResult};
use crate::types::*;

/// The live API.
pub const DEFAULT_BASE_URL: &str = "https://dapp.filobook.run";
/// Per-request timeout unless set otherwise.
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(15);

/// Anything that can go wrong calling the API.
#[derive(Debug)]
#[non_exhaustive]
pub enum Error {
    /// A non-2xx answer. `code` is the API's machine-readable error (e.g. `rate_limited`,
    /// `no_verified_quote`), or `http_error` when the body had none.
    Api {
        /// HTTP status.
        status: u16,
        /// The API's `error` field.
        code: String,
        /// The API's `message` field.
        message: String,
    },
    /// Network failure, timeout, or a client that could not be built.
    Http(reqwest::Error),
    /// A request body that could not be serialised.
    Encode(serde_json::Error),
    /// A 2xx answer whose body did not match the expected shape.
    Decode {
        /// HTTP status.
        status: u16,
        /// The parse error.
        source: serde_json::Error,
    },
}

impl Error {
    /// HTTP status, if the server answered.
    pub fn status(&self) -> Option<u16> {
        match self {
            Self::Api { status, .. } | Self::Decode { status, .. } => Some(*status),
            Self::Http(e) => e.status().map(|s| s.as_u16()),
            Self::Encode(_) => None,
        }
    }

    /// The API error code, for [`Error::Api`].
    pub fn code(&self) -> Option<&str> {
        match self {
            Self::Api { code, .. } => Some(code),
            _ => None,
        }
    }
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Api {
                status,
                code,
                message,
            } => write!(f, "FiloBook API error {status} {code}: {message}"),
            Self::Http(e) => write!(f, "HTTP error: {e}"),
            Self::Encode(e) => write!(f, "could not encode request body: {e}"),
            Self::Decode { status, source } => {
                write!(f, "could not decode response (HTTP {status}): {source}")
            }
        }
    }
}

impl std::error::Error for Error {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Self::Api { .. } => None,
            Self::Http(e) => Some(e),
            Self::Encode(e) => Some(e),
            Self::Decode { source, .. } => Some(source),
        }
    }
}

impl From<reqwest::Error> for Error {
    fn from(e: reqwest::Error) -> Self {
        Self::Http(e)
    }
}

/// Builds a [`FiloBook`] client.
#[derive(Debug, Clone)]
pub struct ClientBuilder {
    base_url: String,
    api_key: Option<String>,
    timeout: Duration,
    user_agent: String,
    http: Option<reqwest::Client>,
}

impl Default for ClientBuilder {
    fn default() -> Self {
        Self {
            base_url: DEFAULT_BASE_URL.to_owned(),
            api_key: None,
            timeout: DEFAULT_TIMEOUT,
            user_agent: format!("filobook-sdk-rust/{}", crate::VERSION),
            http: None,
        }
    }
}

impl ClientBuilder {
    /// An API key (`fb_live_…`), created in the dapp under API keys. Needed for anything that posts or
    /// reads "me". It acts as the wallet that created it.
    pub fn api_key(mut self, key: impl Into<String>) -> Self {
        self.api_key = Some(key.into());
        self
    }

    /// API base URL (default [`DEFAULT_BASE_URL`]). Trailing slashes are removed.
    pub fn base_url(mut self, url: impl Into<String>) -> Self {
        self.base_url = url.into();
        self
    }

    /// Per-request timeout (default 15 s). Ignored when you pass your own client with [`Self::http_client`].
    pub fn timeout(mut self, timeout: Duration) -> Self {
        self.timeout = timeout;
        self
    }

    /// User agent (default `filobook-sdk-rust/<version>`).
    pub fn user_agent(mut self, ua: impl Into<String>) -> Self {
        self.user_agent = ua.into();
        self
    }

    /// Use your own `reqwest::Client` (proxies, connection pool, custom TLS).
    pub fn http_client(mut self, client: reqwest::Client) -> Self {
        self.http = Some(client);
        self
    }

    /// Build the client.
    pub fn build(self) -> Result<FiloBook, Error> {
        let http = match self.http {
            Some(c) => c,
            None => reqwest::Client::builder().timeout(self.timeout).build()?,
        };
        Ok(FiloBook {
            base_url: self.base_url.trim_end_matches('/').to_owned(),
            api_key: self.api_key,
            user_agent: self.user_agent,
            http,
        })
    }
}

/// Options for [`FiloBook::verify_ledger_with`].
#[derive(Debug, Clone, Copy)]
pub struct VerifyLedgerOptions {
    /// Ledger entries per request (default 500, the API's page size).
    pub page_size: u32,
    /// Fills fetched at the same time (default 16).
    pub concurrency: usize,
}

impl Default for VerifyLedgerOptions {
    fn default() -> Self {
        Self {
            page_size: 500,
            concurrency: 16,
        }
    }
}

#[derive(Deserialize)]
struct ApiErrorBody {
    error: Option<String>,
    message: Option<String>,
}

type Query = Vec<(&'static str, String)>;

/// Client for the FiloBook API. Cheap to clone.
#[derive(Debug, Clone)]
pub struct FiloBook {
    base_url: String,
    api_key: Option<String>,
    user_agent: String,
    http: reqwest::Client,
}

/// `encodeURIComponent` for a path segment.
fn encode_segment(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for b in s.bytes() {
        if b.is_ascii_alphanumeric() || b"-_.!~*'()".contains(&b) {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    out
}

fn push<T: ToString>(q: &mut Query, key: &'static str, value: Option<T>) {
    if let Some(v) = value {
        q.push((key, v.to_string()));
    }
}

#[derive(Deserialize)]
struct Orders {
    orders: Vec<Order>,
}
#[derive(Deserialize)]
struct Fills {
    fills: Vec<Fill>,
}
#[derive(Deserialize)]
struct Entries {
    entries: Vec<LedgerEntry>,
}
#[derive(Deserialize)]
struct Providers {
    providers: Vec<Provider>,
}
#[derive(Deserialize)]
struct Batches {
    batches: Vec<BatchListItem>,
}
#[derive(Deserialize)]
struct Quotes {
    quotes: Vec<Quote>,
}

impl FiloBook {
    /// A client for the live API without an API key (public reads only).
    pub fn new() -> Result<Self, Error> {
        Self::builder().build()
    }

    /// A client for the live API with an API key.
    pub fn with_api_key(key: impl Into<String>) -> Result<Self, Error> {
        Self::builder().api_key(key).build()
    }

    /// Start building a client.
    pub fn builder() -> ClientBuilder {
        ClientBuilder::default()
    }

    /// The base URL in use, without a trailing slash.
    pub fn base_url(&self) -> &str {
        &self.base_url
    }

    async fn request<T: DeserializeOwned>(
        &self,
        method: Method,
        path: &str,
        query: Query,
        body: Option<&(dyn erased::Body + Sync)>,
    ) -> Result<T, Error> {
        let mut req = self
            .http
            .request(method, format!("{}{}", self.base_url, path))
            .header(reqwest::header::ACCEPT, "application/json")
            .header(reqwest::header::USER_AGENT, &self.user_agent);
        if !query.is_empty() {
            req = req.query(&query);
        }
        if let Some(key) = &self.api_key {
            req = req.bearer_auth(key);
        }
        if let Some(body) = body {
            let json = body.to_json().map_err(Error::Encode)?;
            req = req
                .header(reqwest::header::CONTENT_TYPE, "application/json")
                .body(json);
        }
        let res = req.send().await?;
        let status = res.status();
        let bytes = res.bytes().await?;
        if !status.is_success() {
            return Err(api_error(status, &bytes));
        }
        let bytes: &[u8] = if bytes.is_empty() { b"null" } else { &bytes };
        serde_json::from_slice(bytes).map_err(|source| Error::Decode {
            status: status.as_u16(),
            source,
        })
    }

    async fn get<T: DeserializeOwned>(&self, path: &str, query: Query) -> Result<T, Error> {
        self.request(Method::GET, path, query, None).await
    }

    async fn post<T: DeserializeOwned>(
        &self,
        path: &str,
        body: Option<&(dyn erased::Body + Sync)>,
    ) -> Result<T, Error> {
        self.request(Method::POST, path, Vec::new(), body).await
    }

    // ---------- public reads ----------

    /// `GET /api/health`.
    pub async fn health(&self) -> Result<Health, Error> {
        self.get("/api/health", Vec::new()).await
    }

    /// Open orders, newest first.
    pub async fn book(
        &self,
        request_type: Option<RequestType>,
        limit: Option<u32>,
    ) -> Result<Vec<Order>, Error> {
        let mut q = Query::new();
        push(&mut q, "type", request_type.map(RequestType::as_str));
        push(&mut q, "limit", limit);
        Ok(self.get::<Orders>("/v1/book", q).await?.orders)
    }

    /// One order with every quote (ranked by price) and its fill, if any.
    pub async fn order(&self, id: &str) -> Result<OrderDetail, Error> {
        self.get(&format!("/v1/orders/{}", encode_segment(id)), Vec::new())
            .await
    }

    /// Recent fills.
    pub async fn fills(&self, limit: Option<u32>) -> Result<Vec<Fill>, Error> {
        let mut q = Query::new();
        push(&mut q, "limit", limit);
        Ok(self.get::<Fills>("/v1/fills", q).await?.fills)
    }

    /// One fill.
    pub async fn fill(&self, id: &str) -> Result<Fill, Error> {
        self.get(&format!("/v1/fills/{}", encode_segment(id)), Vec::new())
            .await
    }

    /// Ledger entries, newest first; with `after` (a height), oldest first from that height, for walking
    /// the chain.
    pub async fn ledger(
        &self,
        after: Option<u64>,
        limit: Option<u32>,
    ) -> Result<Vec<LedgerEntry>, Error> {
        let mut q = Query::new();
        push(&mut q, "after", after);
        push(&mut q, "limit", limit);
        Ok(self.get::<Entries>("/v1/ledger", q).await?.entries)
    }

    /// The server's own walk of the chain. [`Self::verify_ledger`] does the same walk here, without
    /// trusting the server.
    pub async fn ledger_server_check(&self) -> Result<LedgerServerCheck, Error> {
        self.get("/v1/ledger/verify", Vec::new()).await
    }

    /// Filled prices over a window, with the median.
    pub async fn prices(
        &self,
        request_type: Option<RequestType>,
        window: Option<PriceWindow>,
    ) -> Result<Prices, Error> {
        let mut q = Query::new();
        push(&mut q, "type", request_type.map(RequestType::as_str));
        push(&mut q, "window", window.map(PriceWindow::as_str));
        self.get("/v1/prices", q).await
    }

    /// Registered providers.
    pub async fn providers(&self) -> Result<Vec<Provider>, Error> {
        Ok(self
            .get::<Providers>("/v1/providers", Vec::new())
            .await?
            .providers)
    }

    // ---------- agents ----------

    /// Post an order. Needs an API key.
    pub async fn create_order(&self, input: &CreateOrderInput) -> Result<Order, Error> {
        self.post("/v1/orders", Some(input)).await
    }

    /// Up to 20 orders at once; all are validated first, and either every order is created or none.
    pub async fn create_batch(&self, input: &CreateBatchInput) -> Result<Batch, Error> {
        self.post("/v1/orders/batch", Some(input)).await
    }

    /// One batch with its orders.
    pub async fn batch(&self, id: &str) -> Result<Batch, Error> {
        self.get(&format!("/v1/batches/{}", encode_segment(id)), Vec::new())
            .await
    }

    /// Your batches.
    pub async fn my_batches(&self, limit: Option<u32>) -> Result<Vec<BatchListItem>, Error> {
        let mut q = Query::new();
        push(&mut q, "limit", limit);
        Ok(self.get::<Batches>("/v1/me/batches", q).await?.batches)
    }

    /// Cancel every open order in a batch.
    pub async fn cancel_batch(&self, id: &str) -> Result<Batch, Error> {
        self.post(&format!("/v1/batches/{}/cancel", encode_segment(id)), None)
            .await
    }

    /// Fill every open order in the batch with its cheapest verified quote.
    pub async fn fill_batch(&self, id: &str) -> Result<BatchFillResult, Error> {
        self.post(&format!("/v1/batches/{}/fill", encode_segment(id)), None)
            .await
    }

    /// Cancel an open order.
    pub async fn cancel_order(&self, id: &str) -> Result<Order, Error> {
        self.post(&format!("/v1/orders/{}/cancel", encode_segment(id)), None)
            .await
    }

    /// Fill now with the cheapest verified quote at or under the max. Fails with 409
    /// `no_verified_quote` or `conditions_not_met`.
    pub async fn fill_order(&self, id: &str) -> Result<Fill, Error> {
        self.post(&format!("/v1/orders/{}/fill", encode_segment(id)), None)
            .await
    }

    /// Your orders.
    pub async fn my_orders(&self, limit: Option<u32>) -> Result<Vec<Order>, Error> {
        let mut q = Query::new();
        push(&mut q, "limit", limit);
        Ok(self.get::<Orders>("/v1/me/orders", q).await?.orders)
    }

    /// Fills where you were the agent or the provider.
    pub async fn my_fills(&self, limit: Option<u32>) -> Result<Vec<Fill>, Error> {
        let mut q = Query::new();
        push(&mut q, "limit", limit);
        Ok(self.get::<Fills>("/v1/me/fills", q).await?.fills)
    }

    // ---------- providers ----------

    /// Register the API key's wallet as a provider.
    pub async fn register_provider(&self, input: &ProviderInput) -> Result<ProviderMe, Error> {
        self.post("/v1/providers", Some(input)).await
    }

    /// Your provider profile, including the webhook secret.
    pub async fn my_provider(&self) -> Result<ProviderMe, Error> {
        self.get("/v1/providers/me", Vec::new()).await
    }

    /// Change any provider field. `webhook_url: Some(None)` removes the webhook and its secret.
    pub async fn update_provider(&self, patch: &ProviderPatch) -> Result<ProviderMe, Error> {
        self.request(Method::PATCH, "/v1/providers/me", Vec::new(), Some(patch))
            .await
    }

    /// Replace the webhook secret.
    pub async fn rotate_webhook_secret(&self) -> Result<ProviderMe, Error> {
        self.post("/v1/providers/me/webhook/rotate", None).await
    }

    /// Send a signed `test` event to your webhook now.
    pub async fn test_webhook(&self) -> Result<WebhookTestResult, Error> {
        self.post("/v1/providers/me/webhook/test", None).await
    }

    /// Open orders you can quote. With `since` (the cursor from the previous call), only newer orders,
    /// oldest first; poll it alongside the webhook, which is best effort.
    pub async fn feed(&self, since: Option<&str>, limit: Option<u32>) -> Result<FeedPage, Error> {
        let mut q = Query::new();
        push(&mut q, "since", since);
        push(&mut q, "limit", limit);
        self.get("/v1/feed", q).await
    }

    /// Quote on an order. A re-quote must be strictly lower than your current verified quote.
    pub async fn quote(&self, order_id: &str, input: &QuoteInput) -> Result<Quote, Error> {
        self.post(
            &format!("/v1/orders/{}/quotes", encode_segment(order_id)),
            Some(input),
        )
        .await
    }

    /// Your quotes.
    pub async fn my_quotes(&self, limit: Option<u32>) -> Result<Vec<Quote>, Error> {
        let mut q = Query::new();
        push(&mut q, "limit", limit);
        Ok(self.get::<Quotes>("/v1/me/quotes", q).await?.quotes)
    }

    // ---------- ledger ----------

    /// Walk the whole ledger from genesis and recompute every hash here, the same check as "Verify chain"
    /// in the dapp. Uses [`VerifyLedgerOptions::default`].
    pub async fn verify_ledger(&self) -> Result<ChainResult, Error> {
        self.verify_ledger_with(VerifyLedgerOptions::default(), |_| {})
            .await
    }

    /// [`Self::verify_ledger`] with options. `on_progress` gets the verified height after each page.
    ///
    /// Fetches `page_size` entries at a time (`GET /v1/ledger?after=&limit=`) plus the fill behind each
    /// one; a 404 for a fill is reported as `missing_fill`.
    pub async fn verify_ledger_with(
        &self,
        opts: VerifyLedgerOptions,
        mut on_progress: impl FnMut(u64),
    ) -> Result<ChainResult, Error> {
        let page_size = opts.page_size.max(1);
        let mut pos = ChainPosition::genesis();
        loop {
            let entries = self.ledger(Some(pos.height), Some(page_size)).await?;
            if entries.is_empty() {
                return Ok(ChainResult::ok_at(pos));
            }
            let refs: BTreeSet<&str> = entries.iter().map(|e| e.fill_ref.as_str()).collect();
            let fills: HashMap<String, Fill> = stream::iter(refs)
                .map(|id| async move {
                    match self.fill(id).await {
                        Ok(f) => Ok(Some((id.to_owned(), f))),
                        Err(Error::Api { status: 404, .. }) => Ok(None),
                        Err(e) => Err(e),
                    }
                })
                .buffer_unordered(opts.concurrency.max(1))
                .try_filter_map(|x| async move { Ok(x) })
                .try_collect()
                .await?;
            let result = verify_entries_from(&entries, &fills, pos);
            if !result.ok {
                return Ok(result);
            }
            pos = result.position();
            on_progress(pos.height);
        }
    }
}

fn api_error(status: StatusCode, body: &[u8]) -> Error {
    let parsed: Option<ApiErrorBody> = serde_json::from_slice(body).ok();
    let (code, message) = match parsed {
        Some(b) => (b.error, b.message),
        None => (None, None),
    };
    Error::Api {
        status: status.as_u16(),
        code: code.unwrap_or_else(|| "http_error".to_owned()),
        message: message.unwrap_or_else(|| format!("HTTP {}", status.as_u16())),
    }
}

/// Object-safe "serialise to JSON" so request bodies of different types share one code path.
mod erased {
    pub trait Body {
        fn to_json(&self) -> Result<Vec<u8>, serde_json::Error>;
    }
    impl<T: serde::Serialize> Body for T {
        fn to_json(&self) -> Result<Vec<u8>, serde_json::Error> {
            serde_json::to_vec(self)
        }
    }
}
