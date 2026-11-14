from __future__ import annotations

import json
import threading
import urllib.parse
from collections.abc import Callable
from typing import Any

import pytest
from conftest import load_vectors

from filobook import FiloBook, FiloBookError, HttpRequest, HttpResponse

LEDGER = load_vectors("ledger")

Handler = Callable[[urllib.parse.SplitResult, HttpRequest], tuple[int, Any]]


class FakeTransport:
    """Records every request and answers with handler(url, request) -> (status, json body)."""

    def __init__(self, handler: Handler) -> None:
        self.handler = handler
        self.calls: list[HttpRequest] = []
        self._lock = threading.Lock()

    def __call__(self, req: HttpRequest) -> HttpResponse:
        with self._lock:
            self.calls.append(req)
        status, body = self.handler(urllib.parse.urlsplit(req.url), req)
        raw = body if isinstance(body, bytes) else json.dumps(body).encode()
        return HttpResponse(status=status, body=raw)


def query(url: urllib.parse.SplitResult) -> dict[str, str]:
    return dict(urllib.parse.parse_qsl(url.query))


def test_sends_api_key_json_body_and_headers() -> None:
    t = FakeTransport(lambda u, r: (201, {"id": "ord_1"}))
    fb = FiloBook("fb_live_test", transport=t, base_url="https://example.test/")
    out: Any = fb.create_order({"request": "r", "type": "web_search", "max_price": "0.05", "quote_window_s": 30})
    assert out == {"id": "ord_1"}
    req = t.calls[0]
    assert req.url == "https://example.test/v1/orders"
    assert req.method == "POST"
    assert req.headers["authorization"] == "Bearer fb_live_test"
    assert req.headers["content-type"] == "application/json"
    assert req.headers["user-agent"] == "filobook-sdk-python/0.1.0"
    assert req.headers["accept"] == "application/json"
    assert req.timeout == 15.0
    assert req.body is not None
    assert json.loads(req.body) == {"request": "r", "type": "web_search", "max_price": "0.05", "quote_window_s": 30}


def test_drops_none_query_params_and_unwraps_lists() -> None:
    t = FakeTransport(lambda u, r: (200, {"orders": [{"id": "a"}], "cursor": None}))
    fb = FiloBook(transport=t)
    orders: Any = fb.book(type="inference", limit=None)
    assert orders == [{"id": "a"}]
    req = t.calls[0]
    assert req.url == "https://dapp.filobook.run/v1/book?type=inference"
    assert "authorization" not in req.headers
    assert "content-type" not in req.headers
    assert req.body is None


def test_keeps_zero_query_params() -> None:
    t = FakeTransport(lambda u, r: (200, {"entries": []}))
    FiloBook(transport=t).ledger(after=0, limit=5)
    assert t.calls[0].url == "https://dapp.filobook.run/v1/ledger?after=0&limit=5"


def test_feed_is_not_unwrapped() -> None:
    t = FakeTransport(lambda u, r: (200, {"orders": [], "cursor": "c1"}))
    page: Any = FiloBook("k", transport=t).feed(since="c0")
    assert page == {"orders": [], "cursor": "c1"}
    assert t.calls[0].url.endswith("/v1/feed?since=c0")


@pytest.mark.parametrize(
    ("call", "method", "path", "key"),
    [
        (lambda fb: fb.fills(limit=3), "GET", "/v1/fills?limit=3", "fills"),
        (lambda fb: fb.providers(), "GET", "/v1/providers", "providers"),
        (lambda fb: fb.my_batches(), "GET", "/v1/me/batches", "batches"),
        (lambda fb: fb.my_orders(limit=2), "GET", "/v1/me/orders?limit=2", "orders"),
        (lambda fb: fb.my_fills(), "GET", "/v1/me/fills", "fills"),
        (lambda fb: fb.my_quotes(), "GET", "/v1/me/quotes", "quotes"),
    ],
)
def test_list_endpoints(call: Callable[[FiloBook], Any], method: str, path: str, key: str) -> None:
    t = FakeTransport(lambda u, r: (200, {key: [1, 2]}))
    assert call(FiloBook("k", transport=t)) == [1, 2]
    assert t.calls[0].method == method
    assert t.calls[0].url == "https://dapp.filobook.run" + path


@pytest.mark.parametrize(
    ("call", "method", "path", "body"),
    [
        (lambda fb: fb.health(), "GET", "/api/health", None),
        (lambda fb: fb.order("ord_1"), "GET", "/v1/orders/ord_1", None),
        (lambda fb: fb.fill("fil/1"), "GET", "/v1/fills/fil%2F1", None),
        (lambda fb: fb.ledger_server_check(), "GET", "/v1/ledger/verify", None),
        (lambda fb: fb.prices(type="web_search", window="7d"), "GET", "/v1/prices?type=web_search&window=7d", None),
        (lambda fb: fb.create_batch({"orders": []}), "POST", "/v1/orders/batch", {"orders": []}),
        (lambda fb: fb.batch("bat_1"), "GET", "/v1/batches/bat_1", None),
        (lambda fb: fb.cancel_batch("bat_1"), "POST", "/v1/batches/bat_1/cancel", None),
        (lambda fb: fb.fill_batch("bat_1"), "POST", "/v1/batches/bat_1/fill", None),
        (lambda fb: fb.cancel_order("ord_1"), "POST", "/v1/orders/ord_1/cancel", None),
        (lambda fb: fb.fill_order("ord_1"), "POST", "/v1/orders/ord_1/fill", None),
        (
            lambda fb: fb.register_provider({"name": "n", "endpoint_url": "https://x", "types": ["tool"]}),
            "POST",
            "/v1/providers",
            {"name": "n", "endpoint_url": "https://x", "types": ["tool"]},
        ),
        (lambda fb: fb.my_provider(), "GET", "/v1/providers/me", None),
        (lambda fb: fb.update_provider({"webhook_url": None}), "PATCH", "/v1/providers/me", {"webhook_url": None}),
        (lambda fb: fb.rotate_webhook_secret(), "POST", "/v1/providers/me/webhook/rotate", None),
        (lambda fb: fb.test_webhook(), "POST", "/v1/providers/me/webhook/test", None),
        (
            lambda fb: fb.quote("ord_1", {"price": "0.01", "ttl_s": 60, "private": True}),
            "POST",
            "/v1/orders/ord_1/quotes",
            {"price": "0.01", "ttl_s": 60, "private": True},
        ),
    ],
)
def test_endpoints(call: Callable[[FiloBook], Any], method: str, path: str, body: Any) -> None:
    t = FakeTransport(lambda u, r: (200, {"ok": True}))
    assert call(FiloBook("k", transport=t)) == {"ok": True}
    req = t.calls[0]
    assert (req.method, req.url) == (method, "https://dapp.filobook.run" + path)
    assert (json.loads(req.body) if req.body is not None else None) == body


def test_raises_filobook_error_with_api_code() -> None:
    t = FakeTransport(lambda u, r: (429, {"error": "rate_limited", "message": "Too many requests"}))
    with pytest.raises(FiloBookError) as info:
        FiloBook(transport=t).quote("ord_1", {"price": "0.01"})
    err = info.value
    assert (err.status, err.code, err.message, str(err)) == (429, "rate_limited", "Too many requests", "Too many requests")


def test_error_without_json_body() -> None:
    t = FakeTransport(lambda u, r: (502, b"<html>Bad gateway</html>"))
    with pytest.raises(FiloBookError) as info:
        FiloBook(transport=t).health()
    assert (info.value.status, info.value.code, info.value.message) == (502, "http_error", "HTTP 502")


def _ledger_handler(entries: list[dict[str, Any]], fills: dict[str, Any]) -> Handler:
    def handler(u: urllib.parse.SplitResult, r: HttpRequest) -> tuple[int, Any]:
        if u.path == "/v1/ledger":
            q = query(u)
            after, limit = int(q["after"]), int(q.get("limit", 500))
            return 200, {"entries": [e for e in entries if e["height"] > after][:limit]}
        fid = urllib.parse.unquote(u.path.rsplit("/", 1)[-1])
        if fid in fills:
            return 200, fills[fid]
        return 404, {"error": "not_found", "message": "Fill not found"}

    return handler


def test_verify_ledger_walks_pages_and_fetches_fills() -> None:
    valid = LEDGER["cases"][0]
    fills = {f["id"]: f for f in valid["fills"]}
    t = FakeTransport(_ledger_handler(valid["entries"], fills))
    progress: list[int] = []
    res = FiloBook(transport=t).verify_ledger(page_size=2, on_progress=progress.append)
    assert res.ok
    assert res.to_dict() == valid["expect"]
    assert progress == [2, 3]
    ledger_urls = [c.url for c in t.calls if "/v1/ledger" in c.url]
    assert ledger_urls == [
        "https://dapp.filobook.run/v1/ledger?after=0&limit=2",
        "https://dapp.filobook.run/v1/ledger?after=2&limit=2",
        "https://dapp.filobook.run/v1/ledger?after=3&limit=2",
    ]
    assert sorted(c.url.rsplit("/", 1)[-1] for c in t.calls if "/v1/fills/" in c.url) == sorted(fills)


def test_verify_ledger_empty() -> None:
    t = FakeTransport(lambda u, r: (200, {"entries": []}))
    res = FiloBook(transport=t).verify_ledger()
    assert res.to_dict() == LEDGER["cases"][1]["expect"]


def test_verify_ledger_reports_missing_fill() -> None:
    c = next(x for x in LEDGER["cases"] if x["name"] == "fill 3 missing")
    fills = {f["id"]: f for f in c["fills"]}
    t = FakeTransport(_ledger_handler(c["entries"], fills))
    res = FiloBook(transport=t).verify_ledger(concurrency=1)
    assert res.to_dict() == c["expect"]
    assert res.reason == "missing_fill"


def test_verify_ledger_reports_tampering() -> None:
    c = next(x for x in LEDGER["cases"] if x["name"] == "price edited in fill 2")
    fills = {f["id"]: f for f in c["fills"]}
    res = FiloBook(transport=FakeTransport(_ledger_handler(c["entries"], fills))).verify_ledger(page_size=1)
    assert res.to_dict() == c["expect"]


def test_verify_ledger_propagates_other_errors() -> None:
    valid = LEDGER["cases"][0]

    def handler(u: urllib.parse.SplitResult, r: HttpRequest) -> tuple[int, Any]:
        if u.path == "/v1/ledger":
            return 200, {"entries": valid["entries"]}
        return 500, {"error": "internal", "message": "boom"}

    with pytest.raises(FiloBookError) as info:
        FiloBook(transport=FakeTransport(handler)).verify_ledger()
    assert info.value.code == "internal"
