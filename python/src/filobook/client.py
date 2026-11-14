"""HTTP client for the FiloBook API. Standard library only (urllib)."""

from __future__ import annotations

import json
import urllib.error
import urllib.parse
import urllib.request
from collections.abc import Callable, Mapping
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Any, cast

from .errors import FiloBookError
from .ledger import GENESIS, ChainResult, verify_entries
from .types import (
    Batch,
    BatchFillResult,
    BatchListItem,
    CreateBatchInput,
    CreateOrderInput,
    FeedPage,
    Fill,
    Health,
    LedgerEntry,
    LedgerServerCheck,
    Order,
    OrderDetail,
    Prices,
    PriceWindow,
    Provider,
    ProviderInput,
    ProviderMe,
    ProviderPatch,
    Quote,
    QuoteInput,
    RequestType,
    WebhookTestResult,
)

DEFAULT_BASE_URL = "https://dapp.filobook.run"
VERSION = "0.1.0"
DEFAULT_TIMEOUT_S = 15.0


@dataclass
class HttpRequest:
    """What the client asks a transport to send."""

    method: str
    url: str
    headers: dict[str, str] = field(default_factory=dict)
    body: bytes | None = None
    timeout: float = DEFAULT_TIMEOUT_S


@dataclass
class HttpResponse:
    """What a transport returns: the status code and the raw body. Non-2xx answers are responses, not
    exceptions; network failures may raise."""

    status: int
    body: bytes = b""


Transport = Callable[[HttpRequest], HttpResponse]


def urllib_transport(req: HttpRequest) -> HttpResponse:
    """The default transport, built on urllib.request. Network errors (URLError, timeouts) propagate."""
    r = urllib.request.Request(req.url, data=req.body, method=req.method, headers=req.headers)
    try:
        with urllib.request.urlopen(r, timeout=req.timeout) as resp:
            return HttpResponse(status=resp.status, body=resp.read())
    except urllib.error.HTTPError as e:
        try:
            return HttpResponse(status=e.code, body=e.read() or b"")
        finally:
            e.close()


def _seg(value: str) -> str:
    """One path segment, encoded like JavaScript's encodeURIComponent."""
    return urllib.parse.quote(str(value), safe="-_.!~*'()")


def _query_value(v: Any) -> str:
    if isinstance(v, bool):
        return "true" if v else "false"
    return str(v)


class FiloBook:
    """Client for https://dapp.filobook.run.

    ``api_key`` is an API key (fb_live_...) created in the dapp under Settings -> API keys. It acts as the
    wallet that created it and is needed for anything that posts or reads "me". Public reads work without it.
    """

    def __init__(
        self,
        api_key: str | None = None,
        *,
        base_url: str = DEFAULT_BASE_URL,
        timeout: float = DEFAULT_TIMEOUT_S,
        transport: Transport | None = None,
        user_agent: str | None = None,
    ) -> None:
        self.base_url = base_url.rstrip("/")
        self._api_key = api_key
        self.timeout = timeout
        self._transport: Transport = transport or urllib_transport
        self.user_agent = user_agent or f"filobook-sdk-python/{VERSION}"

    def __repr__(self) -> str:
        return f"FiloBook(base_url={self.base_url!r}, api_key={'set' if self._api_key else None})"

    def _request(self, method: str, path: str, *, query: Mapping[str, Any] | None = None, body: Any = None) -> Any:
        url = self.base_url + path
        params = [(k, _query_value(v)) for k, v in (query or {}).items() if v is not None]
        if params:
            url += "?" + urllib.parse.urlencode(params)
        headers = {"accept": "application/json", "user-agent": self.user_agent}
        if self._api_key:
            headers["authorization"] = f"Bearer {self._api_key}"
        data: bytes | None = None
        if body is not None:
            headers["content-type"] = "application/json"
            data = json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        res = self._transport(HttpRequest(method=method, url=url, headers=headers, body=data, timeout=self.timeout))
        parsed: Any = None
        if res.body:
            try:
                parsed = json.loads(res.body.decode("utf-8"))
            except ValueError:
                parsed = None  # not JSON
        if not 200 <= res.status < 300:
            err = parsed if isinstance(parsed, dict) else {}
            code = err.get("error")
            message = err.get("message")
            raise FiloBookError(
                res.status,
                code if isinstance(code, str) else "http_error",
                message if isinstance(message, str) else f"HTTP {res.status}",
            )
        return parsed

    # ---------- public reads ----------

    def health(self) -> Health:
        return cast(Health, self._request("GET", "/api/health"))

    def book(self, *, type: RequestType | None = None, limit: int | None = None) -> list[Order]:
        """Open orders, newest first."""
        return cast(list[Order], self._request("GET", "/v1/book", query={"type": type, "limit": limit})["orders"])

    def order(self, id: str) -> OrderDetail:
        """One order with every quote (ranked by price) and its fill, if any."""
        return cast(OrderDetail, self._request("GET", f"/v1/orders/{_seg(id)}"))

    def fills(self, *, limit: int | None = None) -> list[Fill]:
        return cast(list[Fill], self._request("GET", "/v1/fills", query={"limit": limit})["fills"])

    def fill(self, id: str) -> Fill:
        return cast(Fill, self._request("GET", f"/v1/fills/{_seg(id)}"))

    def ledger(self, *, after: int | None = None, limit: int | None = None) -> list[LedgerEntry]:
        """Newest first; with ``after`` (a height), oldest first from that height, for walking the chain."""
        return cast(
            list[LedgerEntry],
            self._request("GET", "/v1/ledger", query={"after": after, "limit": limit})["entries"],
        )

    def ledger_server_check(self) -> LedgerServerCheck:
        """The server's own walk of the chain. verify_ledger() does the same walk here, without trusting
        the server."""
        return cast(LedgerServerCheck, self._request("GET", "/v1/ledger/verify"))

    def prices(self, *, type: RequestType | None = None, window: PriceWindow | None = None) -> Prices:
        """Filled prices over a window, with the median."""
        return cast(Prices, self._request("GET", "/v1/prices", query={"type": type, "window": window}))

    def providers(self) -> list[Provider]:
        return cast(list[Provider], self._request("GET", "/v1/providers")["providers"])

    # ---------- agents ----------

    def create_order(self, order: CreateOrderInput) -> Order:
        return cast(Order, self._request("POST", "/v1/orders", body=order))

    def create_batch(self, batch: CreateBatchInput) -> Batch:
        """Up to 20 orders at once; all are validated first, and either every order is created or none."""
        return cast(Batch, self._request("POST", "/v1/orders/batch", body=batch))

    def batch(self, id: str) -> Batch:
        return cast(Batch, self._request("GET", f"/v1/batches/{_seg(id)}"))

    def my_batches(self, *, limit: int | None = None) -> list[BatchListItem]:
        return cast(list[BatchListItem], self._request("GET", "/v1/me/batches", query={"limit": limit})["batches"])

    def cancel_batch(self, id: str) -> Batch:
        return cast(Batch, self._request("POST", f"/v1/batches/{_seg(id)}/cancel"))

    def fill_batch(self, id: str) -> BatchFillResult:
        """Fill every open order in the batch with its cheapest verified quote."""
        return cast(BatchFillResult, self._request("POST", f"/v1/batches/{_seg(id)}/fill"))

    def cancel_order(self, id: str) -> Order:
        return cast(Order, self._request("POST", f"/v1/orders/{_seg(id)}/cancel"))

    def fill_order(self, id: str) -> Fill:
        """Fill now with the cheapest verified quote at or under the max. Raises FiloBookError with code
        no_verified_quote or conditions_not_met (409) when it cannot."""
        return cast(Fill, self._request("POST", f"/v1/orders/{_seg(id)}/fill"))

    def my_orders(self, *, limit: int | None = None) -> list[Order]:
        return cast(list[Order], self._request("GET", "/v1/me/orders", query={"limit": limit})["orders"])

    def my_fills(self, *, limit: int | None = None) -> list[Fill]:
        """Fills where you were the agent or the provider."""
        return cast(list[Fill], self._request("GET", "/v1/me/fills", query={"limit": limit})["fills"])

    # ---------- providers ----------

    def register_provider(self, provider: ProviderInput) -> ProviderMe:
        return cast(ProviderMe, self._request("POST", "/v1/providers", body=provider))

    def my_provider(self) -> ProviderMe:
        return cast(ProviderMe, self._request("GET", "/v1/providers/me"))

    def update_provider(self, patch: ProviderPatch) -> ProviderMe:
        """Change any field. ``{"webhook_url": None}`` removes the webhook and its secret."""
        return cast(ProviderMe, self._request("PATCH", "/v1/providers/me", body=patch))

    def rotate_webhook_secret(self) -> ProviderMe:
        return cast(ProviderMe, self._request("POST", "/v1/providers/me/webhook/rotate"))

    def test_webhook(self) -> WebhookTestResult:
        """Send a signed ``test`` event to your webhook now."""
        return cast(WebhookTestResult, self._request("POST", "/v1/providers/me/webhook/test"))

    def feed(self, *, since: str | None = None, limit: int | None = None) -> FeedPage:
        """Open orders you can quote. With ``since`` (the cursor from the previous call), only newer orders,
        oldest first; use it to poll alongside the webhook, which is best effort."""
        return cast(FeedPage, self._request("GET", "/v1/feed", query={"since": since, "limit": limit}))

    def quote(self, order_id: str, quote: QuoteInput) -> Quote:
        """Quote on an order. A re-quote must be strictly lower than your current verified quote."""
        return cast(Quote, self._request("POST", f"/v1/orders/{_seg(order_id)}/quotes", body=quote))

    def my_quotes(self, *, limit: int | None = None) -> list[Quote]:
        return cast(list[Quote], self._request("GET", "/v1/me/quotes", query={"limit": limit})["quotes"])

    # ---------- ledger ----------

    def _fill_or_none(self, id: str) -> Fill | None:
        try:
            return self.fill(id)
        except FiloBookError as e:
            if e.status == 404:
                return None
            raise

    def verify_ledger(
        self,
        *,
        page_size: int = 500,
        on_progress: Callable[[int], None] | None = None,
        concurrency: int = 8,
    ) -> ChainResult:
        """Walk the whole ledger from genesis and recompute every hash here, the same check as "Verify chain"
        in the dapp. Fetches ``page_size`` entries at a time plus the fill behind each one (``concurrency``
        fills in parallel). A fill that answers 404 is reported as ``missing_fill``."""
        pos = GENESIS
        with ThreadPoolExecutor(max_workers=max(1, concurrency)) as pool:
            while True:
                entries = self.ledger(after=pos.height, limit=page_size)
                if not entries:
                    return ChainResult(ok=True, height=pos.height, head=pos.head)
                refs = list(dict.fromkeys(e["ref"] for e in entries))
                fills = dict(zip(refs, pool.map(self._fill_or_none, refs)))
                result = verify_entries(entries, fills, pos)
                if not result.ok:
                    return result
                pos = result.position
                if on_progress is not None:
                    on_progress(pos.height)
