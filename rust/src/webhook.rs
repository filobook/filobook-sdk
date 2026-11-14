//! Feed webhooks. FiloBook POSTs new orders to a provider's `webhook_url` with
//!
//! ```text
//! x-filobook-event:     orders.created | test
//! x-filobook-timestamp: unix seconds
//! x-filobook-signature: "sha256=" + hex(HMAC-SHA256(webhook_secret, timestamp + "." + raw_body))
//! ```
//!
//! Delivery is best effort (one attempt, no retries), so also poll the feed with `since`.

use std::fmt;

use hmac::{Hmac, Mac};
use sha2::Sha256;

use crate::protocol::hmac_sha256_hex;
use crate::types::FeedEvent;

/// Header carrying the signature.
pub const SIGNATURE_HEADER: &str = "x-filobook-signature";
/// Header carrying the unix timestamp (seconds).
pub const TIMESTAMP_HEADER: &str = "x-filobook-timestamp";
/// Header carrying the event type.
pub const EVENT_HEADER: &str = "x-filobook-event";
/// Default allowed clock difference between FiloBook and you, in seconds.
pub const DEFAULT_TOLERANCE_S: u64 = 300;

/// A delivery that failed verification. Answer it with a 4xx and do not act on it.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum WebhookError {
    /// Missing or badly formed timestamp or signature header, or a body that is not a feed event.
    Malformed(&'static str),
    /// The timestamp is outside the allowed window.
    StaleTimestamp,
    /// The signature does not match.
    BadSignature,
}

impl WebhookError {
    /// The machine-readable code: `malformed`, `stale_timestamp` or `bad_signature`.
    pub fn code(&self) -> &'static str {
        match self {
            Self::Malformed(_) => "malformed",
            Self::StaleTimestamp => "stale_timestamp",
            Self::BadSignature => "bad_signature",
        }
    }
}

impl fmt::Display for WebhookError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Malformed(why) => write!(f, "malformed webhook: {why}"),
            Self::StaleTimestamp => {
                f.write_str("stale_timestamp: timestamp is outside the allowed window")
            }
            Self::BadSignature => f.write_str("bad_signature: signature does not match"),
        }
    }
}

impl std::error::Error for WebhookError {}

/// The signature header value for a body. Useful for tests and for replaying a delivery yourself.
pub fn sign_webhook(secret: &str, timestamp: &str, body: impl AsRef<[u8]>) -> String {
    let mut msg = Vec::with_capacity(timestamp.len() + 1 + body.as_ref().len());
    msg.extend_from_slice(timestamp.as_bytes());
    msg.push(b'.');
    msg.extend_from_slice(body.as_ref());
    format!("sha256={}", hmac_sha256_hex(secret, msg))
}

/// What [`verify_webhook`] checks.
#[derive(Debug, Clone, Copy)]
pub struct VerifyWebhookInput<'a> {
    /// The provider's webhook secret (`whsec_…`).
    pub secret: &'a str,
    /// The raw request body, exactly as received. Do not re-serialise parsed JSON.
    pub body: &'a [u8],
    /// Value of the `x-filobook-timestamp` header.
    pub timestamp: Option<&'a str>,
    /// Value of the `x-filobook-signature` header.
    pub signature: Option<&'a str>,
    /// Allowed clock difference in seconds; [`DEFAULT_TOLERANCE_S`] when `None`.
    pub tolerance_s: Option<u64>,
    /// Current unix time in seconds; the system clock when `None`. Pass it on targets without a clock
    /// (e.g. `wasm32-unknown-unknown`).
    pub now: Option<i64>,
}

impl<'a> VerifyWebhookInput<'a> {
    /// Input with the default tolerance and the system clock.
    pub fn new(
        secret: &'a str,
        body: &'a [u8],
        timestamp: Option<&'a str>,
        signature: Option<&'a str>,
    ) -> Self {
        Self {
            secret,
            body,
            timestamp,
            signature,
            tolerance_s: None,
            now: None,
        }
    }
}

fn is_lower_hex_sig(sig: &str) -> bool {
    sig.strip_prefix("sha256=").is_some_and(|h| {
        h.len() == 64
            && h.bytes()
                .all(|b| b.is_ascii_digit() || (b'a'..=b'f').contains(&b))
    })
}

fn now_unix() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

/// Check headers, clock window and signature, in that order. Returns the raw body on success.
fn check(input: &VerifyWebhookInput<'_>) -> Result<(), WebhookError> {
    let timestamp = input
        .timestamp
        .filter(|t| (1..=12).contains(&t.len()) && t.bytes().all(|b| b.is_ascii_digit()))
        .ok_or(WebhookError::Malformed(
            "missing or invalid x-filobook-timestamp",
        ))?;
    let signature =
        input
            .signature
            .filter(|s| is_lower_hex_sig(s))
            .ok_or(WebhookError::Malformed(
                "missing or invalid x-filobook-signature",
            ))?;

    let ts: i64 = timestamp
        .parse()
        .map_err(|_| WebhookError::Malformed("invalid x-filobook-timestamp"))?;
    let now = input.now.unwrap_or_else(now_unix);
    let tolerance = input.tolerance_s.unwrap_or(DEFAULT_TOLERANCE_S);
    if now.abs_diff(ts) > tolerance {
        return Err(WebhookError::StaleTimestamp);
    }

    let given = hex::decode(&signature["sha256=".len()..])
        .map_err(|_| WebhookError::Malformed("invalid signature hex"))?;
    let mut mac = Hmac::<Sha256>::new_from_slice(input.secret.as_bytes())
        .expect("HMAC takes keys of any length");
    mac.update(timestamp.as_bytes());
    mac.update(b".");
    mac.update(input.body);
    // Constant-time comparison.
    mac.verify_slice(&given)
        .map_err(|_| WebhookError::BadSignature)
}

/// Check a delivery and return the parsed event.
///
/// Order: timestamp is 1–12 digits and the signature matches `^sha256=[0-9a-f]{64}$` (else `malformed`);
/// `|now - timestamp| <= tolerance` (else `stale_timestamp`); constant-time signature comparison (else
/// `bad_signature`); the body parses as a [`FeedEvent`] (else `malformed`).
pub fn verify_webhook(input: &VerifyWebhookInput<'_>) -> Result<FeedEvent, WebhookError> {
    check(input)?;
    serde_json::from_slice(input.body).map_err(|e| {
        if e.is_data() {
            WebhookError::Malformed("body is not a feed event")
        } else {
            WebhookError::Malformed("body is not JSON")
        }
    })
}

/// Like [`verify_webhook`], but returns the body as untyped JSON.
pub fn verify_webhook_json(
    input: &VerifyWebhookInput<'_>,
) -> Result<serde_json::Value, WebhookError> {
    check(input)?;
    serde_json::from_slice(input.body).map_err(|_| WebhookError::Malformed("body is not JSON"))
}
