from __future__ import annotations

from typing import Literal

WebhookErrorCode = Literal["malformed", "stale_timestamp", "bad_signature"]


class FiloBookError(Exception):
    """Any non-2xx answer from the API. ``code`` is the API's machine-readable error, e.g. "rate_limited"."""

    def __init__(self, status: int, code: str, message: str) -> None:
        super().__init__(message)
        self.status = status
        self.code = code
        self.message = message

    def __repr__(self) -> str:
        return f"FiloBookError(status={self.status!r}, code={self.code!r}, message={self.message!r})"


class WebhookError(Exception):
    """A feed webhook that failed verification. Answer it with a 4xx and do not act on it."""

    def __init__(self, code: WebhookErrorCode, message: str) -> None:
        super().__init__(message)
        self.code: WebhookErrorCode = code
        self.message = message

    def __repr__(self) -> str:
        return f"WebhookError(code={self.code!r}, message={self.message!r})"
