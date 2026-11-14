from __future__ import annotations

import json
from typing import Any

import pytest
from conftest import load_vectors

from filobook import (
    GENESIS_HASH,
    PAYLOAD_FIELDS,
    WebhookError,
    canonical_json,
    fill_payload,
    fill_payload_hash,
    format_micro,
    parse_price,
    sign_webhook,
    verify_entries,
    verify_webhook,
    verify_webhook_headers,
)

PRICES = load_vectors("prices")
CANONICAL = load_vectors("canonical_json")
LEDGER = load_vectors("ledger")
WEBHOOK = load_vectors("webhook")


# ---------- prices ----------


@pytest.mark.parametrize("case", PRICES["parse"], ids=lambda c: repr(c["input"]))
def test_parse_price(case: dict[str, Any]) -> None:
    assert parse_price(case["input"]) == case["micro"]


@pytest.mark.parametrize("case", PRICES["format"], ids=lambda c: str(c["micro"]))
def test_format_micro(case: dict[str, Any]) -> None:
    assert format_micro(case["micro"]) == case["text"]


def test_parse_price_extra() -> None:
    assert parse_price("١") is None  # Arabic-Indic digit one: \d would match, the spec does not
    assert parse_price("1\n") == 1_000_000
    assert parse_price("﻿1") == 1_000_000  # JS trim() strips U+FEFF
    assert parse_price("\x1c1") is None  # JS trim() does not strip U+001C (Python's strip() would)
    assert parse_price(1) is None  # type: ignore[arg-type]
    with pytest.raises(TypeError):
        format_micro(1.5)  # type: ignore[arg-type]


# ---------- canonical JSON ----------


@pytest.mark.parametrize("case", CANONICAL["cases"], ids=lambda c: c["name"])
def test_canonical_json(case: dict[str, Any]) -> None:
    assert canonical_json(case["input"]) == case["output"]


def test_canonical_json_matches_js_escaping() -> None:
    assert canonical_json("  \x7f/") == '"  \x7f/"'
    assert canonical_json("\x0b\x1f") == '"\\u000b\\u001f"'
    assert canonical_json("\ud800") == '"\\ud800"'  # lone surrogate, like well-formed JSON.stringify
    assert canonical_json("😀") == '"\U0001f600"'  # a pair is one character to JS
    assert canonical_json({"a": True, "b": False, "c": 1}) == '{"a":true,"b":false,"c":1}'


def test_canonical_json_rejects() -> None:
    for bad in (1.0, 0.5, float("nan"), 2**53, -(2**53), {1: "x"}, {"a": {1, 2}}, b"x"):
        with pytest.raises(TypeError):
            canonical_json(bad)
    assert canonical_json(2**53 - 1) == "9007199254740991"


# ---------- ledger ----------


def test_genesis() -> None:
    assert LEDGER["genesis"] == GENESIS_HASH
    assert tuple(LEDGER["payload_fields"]) == PAYLOAD_FIELDS


@pytest.mark.parametrize("case", LEDGER["payloads"], ids=lambda c: c["fill"]["id"])
def test_payload(case: dict[str, Any]) -> None:
    assert canonical_json(case["fill"]) == case["canonical"]
    assert fill_payload_hash(case["fill"]) == case["payload_hash"]


def test_fill_payload_drops_ledger_fields() -> None:
    fill = dict(LEDGER["cases"][0]["fills"][0])
    assert "ledger_height" in fill
    assert list(fill_payload(fill)) == list(PAYLOAD_FIELDS)


@pytest.mark.parametrize("case", LEDGER["cases"], ids=lambda c: c["name"])
def test_ledger_case(case: dict[str, Any]) -> None:
    by_id = {f["id"]: f for f in case["fills"]}
    assert verify_entries(case["entries"], by_id).to_dict() == case["expect"]


# ---------- webhook ----------


@pytest.mark.parametrize("case", WEBHOOK["sign"], ids=lambda c: c["signature"][7:19])
def test_sign_webhook(case: dict[str, Any]) -> None:
    assert sign_webhook(WEBHOOK["secret"], case["timestamp"], case["body"]) == case["signature"]
    assert sign_webhook(WEBHOOK["secret"], case["timestamp"], case["body"].encode()) == case["signature"]


@pytest.mark.parametrize("case", WEBHOOK["verify"], ids=lambda c: c["name"])
def test_verify_webhook(case: dict[str, Any]) -> None:
    def run() -> Any:
        return verify_webhook(
            WEBHOOK["secret"],
            case["body"],
            case["timestamp"],
            case["signature"],
            now=case["now"],
            tolerance_s=case["tolerance_s"],
        )

    if case["ok"]:
        assert run() == json.loads(case["body"])
    else:
        with pytest.raises(WebhookError) as info:
            run()
        assert info.value.code == case["error"]


def test_verify_webhook_bytes_and_headers() -> None:
    c = WEBHOOK["verify"][0]
    assert verify_webhook(WEBHOOK["secret"], c["body"].encode("utf-8"), c["timestamp"], c["signature"], now=c["now"])
    headers = {"X-FiloBook-Timestamp": c["timestamp"], "X-FiloBook-Signature": c["signature"], "X-FiloBook-Event": "orders.created"}
    assert verify_webhook_headers(WEBHOOK["secret"], c["body"], headers, now=c["now"])["event"] == "orders.created"


def test_verify_webhook_missing_headers() -> None:
    with pytest.raises(WebhookError) as info:
        verify_webhook("x", "{}", None, None)
    assert info.value.code == "malformed"


def test_verify_webhook_rejects_non_json_after_signature() -> None:
    for body in ("not json", "NaN", b"\xff"):
        sig = sign_webhook("s", "100", body)
        with pytest.raises(WebhookError) as info:
            verify_webhook("s", body, "100", sig, now=100)
        assert info.value.code == "malformed"


def test_verify_webhook_rejects_unicode_digits() -> None:
    with pytest.raises(WebhookError) as info:
        verify_webhook("s", "{}", "١٠٠", sign_webhook("s", "100", "{}"), now=100)
    assert info.value.code == "malformed"
