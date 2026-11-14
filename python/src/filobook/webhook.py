"""Feed webhooks. FiloBook POSTs new orders to a provider's webhook_url with

    x-filobook-event: orders.created | test
    x-filobook-timestamp: unix seconds
    x-filobook-signature: "sha256=" + hex(HMAC-SHA256(webhook_secret, timestamp + "." + raw_body))
"""

from __future__ import annotations

import hmac
import json
import re
import time
from collections.abc import Mapping
from typing import NoReturn, cast

from .errors import WebhookError
from .protocol import hmac_sha256_hex
from .types import FeedEvent

SIGNATURE_HEADER = "x-filobook-signature"
TIMESTAMP_HEADER = "x-filobook-timestamp"
EVENT_HEADER = "x-filobook-event"
DEFAULT_TOLERANCE_S = 300
"""Default allowed clock difference between FiloBook and you, in seconds."""

_TIMESTAMP_RE = re.compile(r"[0-9]{1,12}")
_SIGNATURE_RE = re.compile(r"sha256=[0-9a-f]{64}")


def _as_bytes(data: str | bytes) -> bytes:
    return data.encode("utf-8") if isinstance(data, str) else bytes(data)


def sign_webhook(secret: str, timestamp: str, body: str | bytes) -> str:
    """The signature header value for a body. Useful for tests and for replaying a delivery yourself."""
    return "sha256=" + hmac_sha256_hex(secret, _as_bytes(timestamp) + b"." + _as_bytes(body))


def _reject_constant(name: str) -> NoReturn:
    # JSON.parse rejects NaN and Infinity; so do we.
    raise ValueError(f"invalid JSON constant {name}")


def verify_webhook(
    secret: str,
    body: str | bytes,
    timestamp: str | None,
    signature: str | None,
    *,
    tolerance_s: float = DEFAULT_TOLERANCE_S,
    now: float | None = None,
) -> FeedEvent:
    """Check a delivery and return the parsed event. Raises WebhookError when it should be rejected.

    ``body`` must be the raw request body exactly as received; do not re-serialise parsed JSON.
    ``now`` is the current time in unix seconds and defaults to the clock.
    """
    if not isinstance(timestamp, str) or not _TIMESTAMP_RE.fullmatch(timestamp):
        raise WebhookError("malformed", "Missing or invalid x-filobook-timestamp")
    if not isinstance(signature, str) or not _SIGNATURE_RE.fullmatch(signature):
        raise WebhookError("malformed", "Missing or invalid x-filobook-signature")
    current = int(time.time()) if now is None else now
    if abs(current - int(timestamp)) > tolerance_s:
        raise WebhookError("stale_timestamp", "Timestamp is outside the allowed window")
    expected = sign_webhook(secret, timestamp, body)
    if not hmac.compare_digest(expected.encode("ascii"), signature.encode("ascii")):
        raise WebhookError("bad_signature", "Signature does not match")
    try:
        text = body if isinstance(body, str) else bytes(body).decode("utf-8")
        return cast(FeedEvent, json.loads(text, parse_constant=_reject_constant))
    except ValueError:  # JSONDecodeError and UnicodeDecodeError are both ValueErrors
        raise WebhookError("malformed", "Body is not JSON") from None


def verify_webhook_headers(
    secret: str,
    body: str | bytes,
    headers: Mapping[str, str],
    *,
    tolerance_s: float = DEFAULT_TOLERANCE_S,
    now: float | None = None,
) -> FeedEvent:
    """verify_webhook with the timestamp and signature read from a headers mapping (any letter case),
    e.g. ``request.headers`` in Flask, FastAPI/Starlette or Django (``request.headers``)."""
    lower = {k.lower(): v for k, v in headers.items()}
    return verify_webhook(
        secret,
        body,
        lower.get(TIMESTAMP_HEADER),
        lower.get(SIGNATURE_HEADER),
        tolerance_s=tolerance_s,
        now=now,
    )
