"""The parts of the protocol every SDK must compute byte-for-byte the same: price math, canonical JSON,
the ledger hashes and the webhook signature. See spec/PROTOCOL.md; spec/vectors holds the test cases."""

from __future__ import annotations

import hashlib
import hmac
import re
from collections.abc import Mapping
from typing import Any

UNIT = 1_000_000
"""1 USDG = 1,000,000 micro-USDG."""
MAX_PRICE_MICRO = 1_000_000 * UNIT
"""Largest price the API accepts, in micro-USDG (1,000,000 USDG)."""
GENESIS_HASH = "0" * 64
"""prev_hash of the first ledger entry."""
MAX_SAFE_INTEGER = 2**53 - 1
"""Largest integer canonical_json accepts, like JavaScript's Number.isSafeInteger."""

PAYLOAD_FIELDS = ("id", "order_id", "agent", "provider_id", "provider_name", "max_price", "price", "saved", "created_at")
"""The fields of a fill the ledger commits to: every field except ledger_height and ledger_hash."""

# ---------- prices ----------

# Characters JavaScript's String.prototype.trim() removes (WhiteSpace + LineTerminator). Python's str.strip()
# uses a different set (it strips U+001C-U+001F but not U+FEFF), so trim explicitly.
_JS_WHITESPACE = (
    "\u0009\u000a\u000b\u000c\u000d   "
    "           "
    "    　﻿"
)
# [0-9], not \d: Python's \d also matches non-ASCII digits.
_PRICE_RE = re.compile(r"([0-9]{1,7})(?:\.([0-9]{1,6}))?")


def parse_price(text: str) -> int | None:
    """"0.0042" -> 4200. None unless it is a positive decimal with at most 7 integer digits and 6 decimals,
    no more than 1,000,000 USDG."""
    if not isinstance(text, str):
        return None
    m = _PRICE_RE.fullmatch(text.strip(_JS_WHITESPACE))
    if not m:
        return None
    micro = int(m.group(1)) * UNIT + int((m.group(2) or "").ljust(6, "0"))
    if micro <= 0 or micro > MAX_PRICE_MICRO:
        return None
    return micro


def format_micro(micro: int) -> str:
    """4200 -> "0.0042". Exact; trailing zeros dropped."""
    if isinstance(micro, bool) or not isinstance(micro, int):
        raise TypeError("format_micro: micro must be an int")
    sign = "-" if micro < 0 else ""
    whole, frac = divmod(abs(micro), UNIT)
    frac_text = str(frac).rjust(6, "0").rstrip("0")
    return f"{sign}{whole}.{frac_text}" if frac_text else f"{sign}{whole}"


# ---------- canonical JSON ----------

_ESCAPES = {'"': '\\"', "\\": "\\\\", "\b": "\\b", "\f": "\\f", "\n": "\\n", "\r": "\\r", "\t": "\\t"}
# A surrogate pair (two code points in a Python str) is one character to JavaScript; a lone surrogate is
# escaped by JSON.stringify as \udxxx.
_NEEDS_ESCAPE = re.compile('([\ud800-\udbff][\udc00-\udfff])|(["\\\\\x00-\x1f\ud800-\udfff])')


def _escape_match(m: re.Match[str]) -> str:
    pair = m.group(1)
    if pair is not None:
        return chr(0x10000 + ((ord(pair[0]) - 0xD800) << 10) + (ord(pair[1]) - 0xDC00))
    ch = m.group(2)
    return _ESCAPES.get(ch) or f"\\u{ord(ch):04x}"


def _quote(s: str) -> str:
    """A JSON string literal, escaped exactly like JavaScript's JSON.stringify."""
    return '"' + _NEEDS_ESCAPE.sub(_escape_match, s) + '"'


def _utf16_key(s: str) -> bytes:
    # Big-endian UTF-16 bytes compare in the same order as UTF-16 code units (JavaScript's default sort).
    return s.encode("utf-16-be", "surrogatepass")


def canonical_json(value: Any) -> str:
    """JSON with object keys sorted (by UTF-16 code units) at every level and no whitespace. Strings are
    escaped exactly like JSON.stringify. Numbers must be integers; the protocol never hashes floats."""
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, str):
        return _quote(value)
    if isinstance(value, int):
        if abs(value) > MAX_SAFE_INTEGER:
            raise TypeError("canonical_json: numbers must be safe integers")
        return str(int(value))
    if isinstance(value, float):
        raise TypeError("canonical_json: floats are not allowed; numbers must be integers")
    if isinstance(value, (list, tuple)):
        return "[" + ",".join(canonical_json(v) for v in value) + "]"
    if isinstance(value, Mapping):
        for k in value:
            if not isinstance(k, str):
                raise TypeError(f"canonical_json: object keys must be strings, got {type(k).__name__}")
        keys = sorted(value, key=_utf16_key)
        return "{" + ",".join(f"{_quote(k)}:{canonical_json(value[k])}" for k in keys) + "}"
    raise TypeError(f"canonical_json: unsupported type {type(value).__name__}")


# ---------- hashing ----------


def _utf8(data: str | bytes) -> bytes:
    """UTF-8 bytes like JavaScript's TextEncoder: lone surrogates become U+FFFD."""
    if isinstance(data, (bytes, bytearray, memoryview)):
        return bytes(data)
    try:
        return data.encode("utf-8")
    except UnicodeEncodeError:
        # Recombine surrogate pairs and replace lone surrogates with U+FFFD.
        return data.encode("utf-16-le", "surrogatepass").decode("utf-16-le", "replace").encode("utf-8")


def sha256_hex(data: str | bytes) -> str:
    """Lowercase hex SHA-256 of a string (as UTF-8) or bytes."""
    return hashlib.sha256(_utf8(data)).hexdigest()


def hmac_sha256_hex(secret: str | bytes, data: str | bytes) -> str:
    """Lowercase hex HMAC-SHA256 with the secret (as UTF-8) as the key."""
    return hmac.new(_utf8(secret), _utf8(data), hashlib.sha256).hexdigest()


def fill_payload(fill: Mapping[str, Any]) -> dict[str, Any]:
    """The fields of a fill the ledger commits to: every field except ledger_height and ledger_hash."""
    return {k: fill[k] for k in PAYLOAD_FIELDS if k in fill}


def fill_payload_hash(fill: Mapping[str, Any]) -> str:
    """sha256(canonical_json(fill payload)), lowercase hex."""
    return sha256_hex(canonical_json(fill_payload(fill)))


def chain_hash(prev_hash: str, payload_hash: str) -> str:
    """sha256(prev_hash + payload_hash): the two lowercase hex strings concatenated, then hashed."""
    return sha256_hex(prev_hash + payload_hash)
