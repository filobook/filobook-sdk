//! The parts of the protocol every SDK must compute byte for byte the same: price math, canonical JSON,
//! the ledger hashes and HMAC. See `spec/PROTOCOL.md`; `spec/vectors` holds the test cases.

use std::cmp::Ordering;
use std::fmt;
use std::fmt::Write as _;

use hmac::{Hmac, Mac};
use serde::Serialize;
use serde_json::{Number, Value};
use sha2::{Digest, Sha256};

use crate::types::Fill;

/// 1 USDG = 1,000,000 micro-USDG.
pub const UNIT: u64 = 1_000_000;
/// Largest price the API accepts, in micro-USDG (1,000,000 USDG).
pub const MAX_PRICE_MICRO: u64 = 1_000_000 * UNIT;
/// `prev_hash` of the first ledger entry.
pub const GENESIS_HASH: &str = "0000000000000000000000000000000000000000000000000000000000000000";

/// Largest integer JavaScript represents exactly (2^53 - 1). Canonical JSON rejects anything bigger.
const MAX_SAFE_INTEGER: u64 = 9_007_199_254_740_991;

/// Whitespace removed by JavaScript's `String.prototype.trim`: Unicode `White_Space` minus U+0085, plus U+FEFF.
fn is_js_whitespace(c: char) -> bool {
    (c.is_whitespace() && c != '\u{85}') || c == '\u{FEFF}'
}

/// `"0.0042"` → `Some(4200)`.
///
/// Returns `None` unless the trimmed text is a positive decimal with 1–7 integer digits and at most 6
/// decimals, no greater than 1,000,000 USDG. Never uses floating point.
pub fn parse_price(text: &str) -> Option<u64> {
    let t = text.trim_matches(is_js_whitespace);
    let (int_part, frac_part) = match t.split_once('.') {
        Some((i, f)) => (i, Some(f)),
        None => (t, None),
    };
    let digits = |s: &str, max: usize| {
        !s.is_empty() && s.len() <= max && s.bytes().all(|b| b.is_ascii_digit())
    };
    if !digits(int_part, 7) {
        return None;
    }
    let mut micro: u64 = int_part.parse::<u64>().ok()? * UNIT;
    if let Some(f) = frac_part {
        if !digits(f, 6) {
            return None;
        }
        let mut padded = f.to_owned();
        while padded.len() < 6 {
            padded.push('0');
        }
        micro += padded.parse::<u64>().ok()?;
    }
    if micro == 0 || micro > MAX_PRICE_MICRO {
        return None;
    }
    Some(micro)
}

/// `4200` → `"0.0042"`. Exact, trailing zeros dropped, `-` for negative values.
pub fn format_micro(micro: i64) -> String {
    let sign = if micro < 0 { "-" } else { "" };
    let abs = micro.unsigned_abs();
    let whole = abs / UNIT;
    let frac = format!("{:06}", abs % UNIT);
    let frac = frac.trim_end_matches('0');
    if frac.is_empty() {
        format!("{sign}{whole}")
    } else {
        format!("{sign}{whole}.{frac}")
    }
}

/// Why a value has no canonical JSON form.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CanonicalJsonError {
    /// A number that is not an integer within ±(2^53 - 1). The protocol never hashes floats.
    NotSafeInteger(String),
}

impl fmt::Display for CanonicalJsonError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::NotSafeInteger(n) => {
                write!(f, "canonical_json: numbers must be safe integers, got {n}")
            }
        }
    }
}

impl std::error::Error for CanonicalJsonError {}

/// JSON with object keys sorted by UTF-16 code units at every level and no whitespace. Strings are
/// escaped exactly like JavaScript's `JSON.stringify`. Numbers must be integers within ±(2^53 - 1).
pub fn canonical_json(value: &Value) -> Result<String, CanonicalJsonError> {
    let mut out = String::new();
    write_canonical(&mut out, value)?;
    Ok(out)
}

fn write_canonical(out: &mut String, value: &Value) -> Result<(), CanonicalJsonError> {
    match value {
        Value::Null => out.push_str("null"),
        Value::Bool(b) => out.push_str(if *b { "true" } else { "false" }),
        Value::Number(n) => write_number(out, n)?,
        Value::String(s) => write_js_string(out, s),
        Value::Array(items) => {
            out.push('[');
            for (i, item) in items.iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                write_canonical(out, item)?;
            }
            out.push(']');
        }
        Value::Object(map) => {
            let mut keys: Vec<&String> = map.keys().collect();
            keys.sort_by(|a, b| utf16_cmp(a, b));
            out.push('{');
            for (i, key) in keys.into_iter().enumerate() {
                if i > 0 {
                    out.push(',');
                }
                write_js_string(out, key);
                out.push(':');
                write_canonical(out, &map[key])?;
            }
            out.push('}');
        }
    }
    Ok(())
}

fn write_number(out: &mut String, n: &Number) -> Result<(), CanonicalJsonError> {
    let reject = || CanonicalJsonError::NotSafeInteger(n.to_string());
    if let Some(u) = n.as_u64() {
        if u > MAX_SAFE_INTEGER {
            return Err(reject());
        }
        let _ = write!(out, "{u}");
    } else if let Some(i) = n.as_i64() {
        if i.unsigned_abs() > MAX_SAFE_INTEGER {
            return Err(reject());
        }
        let _ = write!(out, "{i}");
    } else {
        // A float. JavaScript would accept it only if it is a safe integer (1.0 → "1", -0.0 → "0").
        let f = n.as_f64().ok_or_else(reject)?;
        if f.fract() != 0.0 || f.abs() > MAX_SAFE_INTEGER as f64 {
            return Err(reject());
        }
        let _ = write!(out, "{}", f as i64);
    }
    Ok(())
}

/// JavaScript's default string order: compare UTF-16 code units.
fn utf16_cmp(a: &str, b: &str) -> Ordering {
    a.encode_utf16().cmp(b.encode_utf16())
}

/// A string literal exactly as `JSON.stringify` writes it.
fn write_js_string(out: &mut String, s: &str) {
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{08}' => out.push_str("\\b"),
            '\u{0C}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            c if (c as u32) < 0x20 => {
                let _ = write!(out, "\\u{:04x}", c as u32);
            }
            c => out.push(c),
        }
    }
    out.push('"');
}

fn to_hex(bytes: &[u8]) -> String {
    hex::encode(bytes)
}

/// SHA-256 of the bytes, lowercase hex.
pub fn sha256_hex(data: impl AsRef<[u8]>) -> String {
    to_hex(&Sha256::digest(data.as_ref()))
}

/// HMAC-SHA256 with the secret's UTF-8 bytes as key, lowercase hex.
pub fn hmac_sha256_hex(secret: &str, data: impl AsRef<[u8]>) -> String {
    let mut mac =
        Hmac::<Sha256>::new_from_slice(secret.as_bytes()).expect("HMAC takes keys of any length");
    mac.update(data.as_ref());
    to_hex(&mac.finalize().into_bytes())
}

/// The fields of a fill the ledger commits to: every field except `ledger_height` and `ledger_hash`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize)]
pub struct FillPayload<'a> {
    #[allow(missing_docs)]
    pub id: &'a str,
    #[allow(missing_docs)]
    pub order_id: &'a str,
    #[allow(missing_docs)]
    pub agent: &'a str,
    #[allow(missing_docs)]
    pub provider_id: &'a str,
    #[allow(missing_docs)]
    pub provider_name: &'a str,
    #[allow(missing_docs)]
    pub max_price: &'a str,
    #[allow(missing_docs)]
    pub price: &'a str,
    #[allow(missing_docs)]
    pub saved: &'a str,
    #[allow(missing_docs)]
    pub created_at: &'a str,
}

/// The ledger payload of a fill.
pub fn fill_payload(fill: &Fill) -> FillPayload<'_> {
    FillPayload {
        id: &fill.id,
        order_id: &fill.order_id,
        agent: &fill.agent,
        provider_id: &fill.provider_id,
        provider_name: &fill.provider_name,
        max_price: &fill.max_price,
        price: &fill.price,
        saved: &fill.saved,
        created_at: &fill.created_at,
    }
}

impl FillPayload<'_> {
    /// The payload's canonical JSON (the bytes that get hashed).
    pub fn canonical_json(&self) -> String {
        let value = serde_json::to_value(self).expect("a fill payload is all strings");
        canonical_json(&value).expect("a fill payload is all strings")
    }

    /// `sha256(canonical_json(payload))`, lowercase hex.
    pub fn hash(&self) -> String {
        sha256_hex(self.canonical_json())
    }
}

/// `sha256(canonical_json(fill payload))`, lowercase hex.
pub fn fill_payload_hash(fill: &Fill) -> String {
    fill_payload(fill).hash()
}

/// `sha256(prev_hash + payload_hash)`: the two lowercase hex strings concatenated, then hashed.
pub fn chain_hash(prev_hash: &str, payload_hash: &str) -> String {
    let mut h = Sha256::new();
    h.update(prev_hash.as_bytes());
    h.update(payload_hash.as_bytes());
    to_hex(&h.finalize())
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn js_trim_rules() {
        assert_eq!(parse_price("\u{FEFF}1\u{3000}"), Some(UNIT));
        assert_eq!(parse_price("\u{85}1"), None);
    }

    #[test]
    fn rejects_unsafe_numbers() {
        assert!(canonical_json(&json!(9_007_199_254_740_992u64)).is_err());
        assert!(canonical_json(&json!(-9_007_199_254_740_992i64)).is_err());
        assert!(canonical_json(&json!(1.5)).is_err());
        assert_eq!(canonical_json(&json!(2.0)).unwrap(), "2");
        assert_eq!(
            canonical_json(&json!(-9_007_199_254_740_991i64)).unwrap(),
            "-9007199254740991"
        );
    }

    #[test]
    fn escapes_like_js() {
        let s = "\u{7f}\u{2028}\u{2029}/\u{1b}";
        assert_eq!(
            canonical_json(&json!(s)).unwrap(),
            "\"\u{7f}\u{2028}\u{2029}/\\u001b\""
        );
    }

    #[test]
    fn format_extremes() {
        assert_eq!(format_micro(i64::MIN), "-9223372036854.775808");
    }
}
