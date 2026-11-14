//! Runs every case in ../spec/vectors (shared by all SDKs in the repo).

use std::collections::HashMap;
use std::path::PathBuf;

use filobook::*;
use serde_json::Value;

fn load(name: &str) -> Value {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../spec/vectors")
        .join(name);
    let text =
        std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("read {}: {e}", path.display()));
    serde_json::from_str(&text).unwrap()
}

fn cases<'a>(v: &'a Value, key: &str) -> &'a Vec<Value> {
    let list = v[key].as_array().unwrap_or_else(|| panic!("missing {key}"));
    assert!(!list.is_empty(), "no cases under {key}");
    list
}

#[test]
fn prices_parse() {
    let v = load("prices.json");
    for c in cases(&v, "parse") {
        let input = c["input"].as_str().unwrap();
        assert_eq!(parse_price(input), c["micro"].as_u64(), "parse {input:?}");
    }
}

#[test]
fn prices_format() {
    let v = load("prices.json");
    for c in cases(&v, "format") {
        let micro = c["micro"].as_i64().unwrap();
        assert_eq!(
            format_micro(micro),
            c["text"].as_str().unwrap(),
            "format {micro}"
        );
    }
}

#[test]
fn canonical_json_cases() {
    let v = load("canonical_json.json");
    for c in cases(&v, "cases") {
        let got = canonical_json(&c["input"]).unwrap();
        assert_eq!(got, c["output"].as_str().unwrap(), "{}", c["name"]);
    }
}

#[test]
fn ledger_genesis() {
    assert_eq!(load("ledger.json")["genesis"], GENESIS_HASH);
}

#[test]
fn ledger_payloads() {
    let v = load("ledger.json");
    let fields: Vec<&str> = cases(&v, "payload_fields")
        .iter()
        .map(|f| f.as_str().unwrap())
        .collect();
    for p in cases(&v, "payloads") {
        // canonical_json of the raw payload object.
        assert_eq!(
            canonical_json(&p["fill"]).unwrap(),
            p["canonical"].as_str().unwrap()
        );
        // The same through the typed Fill.
        let mut obj = p["fill"].clone();
        obj["ledger_height"] = 1.into();
        obj["ledger_hash"] = "x".into();
        let fill: Fill = serde_json::from_value(obj).unwrap();
        let payload = fill_payload(&fill);
        assert_eq!(payload.canonical_json(), p["canonical"].as_str().unwrap());
        assert_eq!(
            fill_payload_hash(&fill),
            p["payload_hash"].as_str().unwrap()
        );
        assert_eq!(
            sha256_hex(p["canonical"].as_str().unwrap()),
            p["payload_hash"].as_str().unwrap()
        );
        let keys: Vec<String> = serde_json::to_value(&payload)
            .unwrap()
            .as_object()
            .unwrap()
            .keys()
            .cloned()
            .collect();
        let mut want: Vec<String> = fields.iter().map(|s| s.to_string()).collect();
        want.sort();
        assert_eq!(keys, want);
    }
}

#[test]
fn ledger_cases() {
    let v = load("ledger.json");
    for c in cases(&v, "cases") {
        let name = c["name"].as_str().unwrap();
        let entries: Vec<LedgerEntry> = serde_json::from_value(c["entries"].clone()).unwrap();
        let fills: Vec<Fill> = serde_json::from_value(c["fills"].clone()).unwrap();
        let by_id: HashMap<String, Fill> = fills.into_iter().map(|f| (f.id.clone(), f)).collect();
        let want: ChainResult = serde_json::from_value(c["expect"].clone()).unwrap();
        let got = verify_entries(&entries, &by_id);
        assert_eq!(got, want, "{name}");
        // The result serialises to exactly the vector's shape.
        assert_eq!(serde_json::to_value(&got).unwrap(), c["expect"], "{name}");
    }
}

#[test]
fn ledger_chain_hashes() {
    let v = load("ledger.json");
    let valid = &cases(&v, "cases")[0];
    for e in valid["entries"].as_array().unwrap() {
        let got = chain_hash(
            e["prev_hash"].as_str().unwrap(),
            e["payload_hash"].as_str().unwrap(),
        );
        assert_eq!(got, e["hash"].as_str().unwrap());
    }
}

#[test]
fn webhook_sign() {
    let v = load("webhook.json");
    let secret = v["secret"].as_str().unwrap();
    for c in cases(&v, "sign") {
        let got = sign_webhook(
            secret,
            c["timestamp"].as_str().unwrap(),
            c["body"].as_str().unwrap(),
        );
        assert_eq!(got, c["signature"].as_str().unwrap());
    }
}

#[test]
fn webhook_verify() {
    let v = load("webhook.json");
    let secret = v["secret"].as_str().unwrap();
    for c in cases(&v, "verify") {
        let name = c["name"].as_str().unwrap();
        let body = c["body"].as_str().unwrap();
        let input = VerifyWebhookInput {
            secret,
            body: body.as_bytes(),
            timestamp: c["timestamp"].as_str(),
            signature: c["signature"].as_str(),
            tolerance_s: c["tolerance_s"].as_u64(),
            now: c["now"].as_i64(),
        };
        if c["ok"].as_bool().unwrap() {
            let want: Value = serde_json::from_str(body).unwrap();
            assert_eq!(verify_webhook_json(&input).unwrap(), want, "{name}");
            let event = verify_webhook(&input).unwrap_or_else(|e| panic!("{name}: {e}"));
            assert_eq!(event.sent_at, want["sent_at"].as_str().unwrap(), "{name}");
            assert_eq!(
                event.orders.len(),
                want["orders"].as_array().unwrap().len(),
                "{name}"
            );
            assert_eq!(
                serde_json::to_value(event.event).unwrap(),
                want["event"],
                "{name}"
            );
        } else {
            let want = c["error"].as_str().unwrap();
            assert_eq!(verify_webhook(&input).unwrap_err().code(), want, "{name}");
            assert_eq!(
                verify_webhook_json(&input).unwrap_err().code(),
                want,
                "{name}"
            );
        }
    }
}

#[test]
fn webhook_missing_headers_and_bad_json() {
    let err = verify_webhook(&VerifyWebhookInput::new("x", b"{}", None, None)).unwrap_err();
    assert_eq!(
        err,
        WebhookError::Malformed("missing or invalid x-filobook-timestamp")
    );

    let body = b"not json";
    let sig = sign_webhook("s", "100", body);
    let input = VerifyWebhookInput {
        now: Some(100),
        ..VerifyWebhookInput::new("s", body, Some("100"), Some(&sig))
    };
    assert_eq!(verify_webhook(&input).unwrap_err().code(), "malformed");

    // A 13-digit timestamp is malformed even if correctly signed.
    let sig = sign_webhook("s", "1000000000000", b"{}");
    let input = VerifyWebhookInput {
        now: Some(0),
        ..VerifyWebhookInput::new("s", b"{}", Some("1000000000000"), Some(&sig))
    };
    assert_eq!(verify_webhook_json(&input).unwrap_err().code(), "malformed");
}
