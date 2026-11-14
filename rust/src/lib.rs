//! Rust SDK for [FiloBook](https://filobook.run), the order book for x402 agent calls.
//!
//! - [`protocol`]: price math (micro-USDG), canonical JSON, SHA-256 and the ledger hashes.
//! - [`ledger`]: recompute and check the fill ledger hash chain.
//! - [`webhook`]: sign and verify feed webhook deliveries.
//! - [`types`]: the API's request and response shapes.
//! - `client` (default cargo feature): an async HTTP client for `https://dapp.filobook.run`.
//!
//! Everything except the client builds with `--no-default-features`, with no async runtime or HTTP stack.
//! The protocol rules are described in `spec/PROTOCOL.md` in the SDK repository.
//!
//! No settlement contract is deployed yet: a fill records who won and at what price, and no funds move.

#![forbid(unsafe_code)]
#![warn(missing_docs)]

pub mod ledger;
pub mod protocol;
pub mod types;
pub mod webhook;

#[cfg(feature = "client")]
mod client;
#[cfg(feature = "client")]
pub use client::{
    ClientBuilder, Error, FiloBook, VerifyLedgerOptions, DEFAULT_BASE_URL, DEFAULT_TIMEOUT,
};

pub use ledger::{verify_entries, verify_entries_from, BreakReason, ChainPosition, ChainResult};
pub use protocol::{
    canonical_json, chain_hash, fill_payload, fill_payload_hash, format_micro, hmac_sha256_hex,
    parse_price, sha256_hex, CanonicalJsonError, FillPayload, GENESIS_HASH, MAX_PRICE_MICRO, UNIT,
};
pub use types::*;
pub use webhook::{
    sign_webhook, verify_webhook, verify_webhook_json, VerifyWebhookInput, WebhookError,
    DEFAULT_TOLERANCE_S, EVENT_HEADER, SIGNATURE_HEADER, TIMESTAMP_HEADER,
};

/// SDK version, sent in the user agent.
pub const VERSION: &str = env!("CARGO_PKG_VERSION");
