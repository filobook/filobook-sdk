//! Client tests against a local mock server (never the live API).
#![cfg(feature = "client")]

use std::path::PathBuf;

use filobook::*;
use serde_json::{json, Value};
use wiremock::matchers::{body_json, header, method, path, query_param};
use wiremock::{Mock, MockServer, Request, Respond, ResponseTemplate};

fn order_json(id: &str) -> Value {
    json!({
        "id": id, "agent": "0x1111111111111111111111111111111111111111", "request": "r", "type": "web_search",
        "max_price": "0.05", "rule": null, "status": "open", "quote_count": 0, "best_quote": null,
        "private_quotes": 0, "conditions": null, "batch_id": null, "close_reason": null,
        "created_at": "2026-01-01T00:00:00.000Z", "closes_at": "2026-01-01T00:01:00.000Z"
    })
}

fn client(server: &MockServer, key: Option<&str>) -> FiloBook {
    let mut b = FiloBook::builder().base_url(format!("{}/", server.uri()));
    if let Some(k) = key {
        b = b.api_key(k);
    }
    b.build().unwrap()
}

#[tokio::test]
async fn sends_api_key_json_body_and_user_agent() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/v1/orders"))
        .and(header("authorization", "Bearer fb_live_test"))
        .and(header("content-type", "application/json"))
        .and(header("user-agent", "filobook-sdk-rust/0.1.0"))
        .and(body_json(json!({ "request": "r", "type": "web_search", "max_price": "0.05", "quote_window_s": 30 })))
        .respond_with(ResponseTemplate::new(201).set_body_json(order_json("ord_1")))
        .expect(1)
        .mount(&server)
        .await;

    let fb = client(&server, Some("fb_live_test"));
    let mut input = CreateOrderInput::new("r", RequestType::WebSearch, "0.05");
    input.quote_window_s = Some(30);
    let order = fb.create_order(&input).await.unwrap();
    assert_eq!(order.id, "ord_1");
    assert_eq!(order.status, OrderStatus::Open);
    assert_eq!(order.request_type, RequestType::WebSearch);
}

#[tokio::test]
async fn query_params_and_no_auth_without_key() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/v1/book"))
        .and(query_param("type", "inference"))
        .and(query_param("limit", "5"))
        .respond_with(
            ResponseTemplate::new(200).set_body_json(json!({ "orders": [order_json("a")] })),
        )
        .expect(1)
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/v1/prices"))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_json(json!({ "points": [], "median": null, "count": 0 })),
        )
        .mount(&server)
        .await;

    let fb = client(&server, None);
    let orders = fb
        .book(Some(RequestType::Inference), Some(5))
        .await
        .unwrap();
    assert_eq!(orders.len(), 1);
    let prices = fb.prices(None, Some(PriceWindow::Last7d)).await.unwrap();
    assert_eq!(prices.count, 0);

    let reqs = server.received_requests().await.unwrap();
    assert!(reqs
        .iter()
        .all(|r| !r.headers.contains_key("authorization")));
    // Empty options are dropped from the query string.
    assert_eq!(reqs[1].url.query(), Some("window=7d"));
}

#[tokio::test]
async fn path_segments_are_encoded() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/v1/fills/a%2Fb%20c"))
        .respond_with(
            ResponseTemplate::new(404)
                .set_body_json(json!({ "error": "not_found", "message": "Fill not found" })),
        )
        .expect(1)
        .mount(&server)
        .await;
    let err = client(&server, None).fill("a/b c").await.unwrap_err();
    assert_eq!(err.status(), Some(404));
    assert_eq!(err.code(), Some("not_found"));
}

#[tokio::test]
async fn api_errors_are_mapped() {
    let server = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/v1/orders/ord_1/quotes"))
        .and(body_json(json!({ "price": "0.01" })))
        .respond_with(
            ResponseTemplate::new(429)
                .set_body_json(json!({ "error": "rate_limited", "message": "Too many requests" })),
        )
        .mount(&server)
        .await;
    Mock::given(method("POST"))
        .and(path("/v1/orders/ord_2/fill"))
        .respond_with(ResponseTemplate::new(502).set_body_string("<html>bad gateway</html>"))
        .mount(&server)
        .await;
    Mock::given(method("GET"))
        .and(path("/api/health"))
        .respond_with(ResponseTemplate::new(200).set_body_string("{\"unexpected\":true}"))
        .mount(&server)
        .await;

    let fb = client(&server, Some("fb_live_test"));
    match fb
        .quote("ord_1", &QuoteInput::new("0.01"))
        .await
        .unwrap_err()
    {
        Error::Api {
            status,
            code,
            message,
        } => {
            assert_eq!(
                (status, code.as_str(), message.as_str()),
                (429, "rate_limited", "Too many requests")
            );
        }
        e => panic!("unexpected {e:?}"),
    }
    match fb.fill_order("ord_2").await.unwrap_err() {
        Error::Api {
            status,
            code,
            message,
        } => {
            assert_eq!(
                (status, code.as_str(), message.as_str()),
                (502, "http_error", "HTTP 502")
            );
        }
        e => panic!("unexpected {e:?}"),
    }
    assert!(matches!(
        fb.health().await.unwrap_err(),
        Error::Decode { status: 200, .. }
    ));
}

#[tokio::test]
async fn provider_patch_sends_null_webhook() {
    let server = MockServer::start().await;
    let me = json!({
        "id": "prv_1", "name": "p", "address": "0x1", "endpoint_url": "https://x.test", "types": ["web_search"],
        "active": true, "quotes_won": 0, "created_at": "2026-01-01T00:00:00.000Z",
        "webhook_url": null, "webhook_secret": null, "webhook_status": null, "webhook_at": null
    });
    Mock::given(method("PATCH"))
        .and(path("/v1/providers/me"))
        .and(body_json(json!({ "active": false, "webhook_url": null })))
        .respond_with(ResponseTemplate::new(200).set_body_json(me))
        .expect(1)
        .mount(&server)
        .await;
    let patch = ProviderPatch {
        active: Some(false),
        webhook_url: Some(None),
        ..Default::default()
    };
    let p = client(&server, Some("fb_live_test"))
        .update_provider(&patch)
        .await
        .unwrap();
    assert_eq!(p.provider.id, "prv_1");
    assert_eq!(p.webhook_url, None);
}

#[tokio::test]
async fn feed_passes_cursor() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/v1/feed"))
        .and(query_param("since", "c1"))
        .respond_with(
            ResponseTemplate::new(200)
                .set_body_json(json!({ "orders": [order_json("o")], "cursor": "c2" })),
        )
        .expect(1)
        .mount(&server)
        .await;
    let page = client(&server, Some("fb_live_test"))
        .feed(Some("c1"), None)
        .await
        .unwrap();
    assert_eq!(page.cursor.as_deref(), Some("c2"));
    assert_eq!(page.orders[0].id, "o");
}

// ---------- verify_ledger ----------

fn valid_chain() -> (Vec<Value>, Vec<Value>) {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../spec/vectors/ledger.json");
    let v: Value = serde_json::from_str(&std::fs::read_to_string(path).unwrap()).unwrap();
    let c = &v["cases"][0];
    assert_eq!(c["name"], "valid chain");
    (
        c["entries"].as_array().unwrap().clone(),
        c["fills"].as_array().unwrap().clone(),
    )
}

/// Serves /v1/ledger?after=&limit= from the entries and /v1/fills/:id from the fills, like the API.
struct LedgerApi {
    entries: Vec<Value>,
    fills: Vec<Value>,
}

impl Respond for LedgerApi {
    fn respond(&self, req: &Request) -> ResponseTemplate {
        let p = req.url.path();
        if p == "/v1/ledger" {
            let q: std::collections::HashMap<_, _> = req.url.query_pairs().into_owned().collect();
            let after: u64 = q["after"].parse().unwrap();
            let limit: usize = q["limit"].parse().unwrap();
            let page: Vec<&Value> = self
                .entries
                .iter()
                .filter(|e| e["height"].as_u64().unwrap() > after)
                .take(limit)
                .collect();
            return ResponseTemplate::new(200).set_body_json(json!({ "entries": page }));
        }
        let id = p.rsplit('/').next().unwrap();
        match self.fills.iter().find(|f| f["id"] == id) {
            Some(f) => ResponseTemplate::new(200).set_body_json(f),
            None => ResponseTemplate::new(404)
                .set_body_json(json!({ "error": "not_found", "message": "Fill not found" })),
        }
    }
}

#[tokio::test]
async fn verify_ledger_walks_pages() {
    let (entries, fills) = valid_chain();
    let head = entries[2]["hash"].as_str().unwrap().to_owned();
    let server = MockServer::start().await;
    Mock::given(wiremock::matchers::any())
        .respond_with(LedgerApi { entries, fills })
        .mount(&server)
        .await;

    let mut progress = Vec::new();
    let res = client(&server, None)
        .verify_ledger_with(
            VerifyLedgerOptions {
                page_size: 2,
                concurrency: 4,
            },
            |h| progress.push(h),
        )
        .await
        .unwrap();
    assert_eq!(
        res,
        ChainResult {
            ok: true,
            height: 3,
            head,
            broken_at: None,
            reason: None
        }
    );
    assert_eq!(progress, vec![2, 3]);

    let pages: Vec<String> = server
        .received_requests()
        .await
        .unwrap()
        .iter()
        .filter(|r| r.url.path() == "/v1/ledger")
        .map(|r| r.url.query().unwrap_or("").to_owned())
        .collect();
    assert_eq!(
        pages,
        vec!["after=0&limit=2", "after=2&limit=2", "after=3&limit=2"]
    );
}

#[tokio::test]
async fn verify_ledger_reports_missing_fill_on_404() {
    let (entries, mut fills) = valid_chain();
    let head1 = entries[0]["hash"].as_str().unwrap().to_owned();
    fills.retain(|f| f["id"] != "fil_000000000002");
    let server = MockServer::start().await;
    Mock::given(wiremock::matchers::any())
        .respond_with(LedgerApi { entries, fills })
        .mount(&server)
        .await;

    let res = client(&server, None).verify_ledger().await.unwrap();
    assert_eq!(
        res,
        ChainResult {
            ok: false,
            height: 1,
            head: head1,
            broken_at: Some(2),
            reason: Some(BreakReason::MissingFill)
        }
    );
    let first = &server.received_requests().await.unwrap()[0];
    assert_eq!(first.url.query(), Some("after=0&limit=500"));
}

#[tokio::test]
async fn verify_ledger_propagates_other_errors() {
    let server = MockServer::start().await;
    Mock::given(method("GET"))
        .and(path("/v1/ledger"))
        .respond_with(
            ResponseTemplate::new(500)
                .set_body_json(json!({ "error": "internal", "message": "boom" })),
        )
        .mount(&server)
        .await;
    let err = client(&server, None).verify_ledger().await.unwrap_err();
    assert_eq!(err.code(), Some("internal"));
}

#[test]
fn base_url_trailing_slash_is_stripped() {
    let fb = FiloBook::builder()
        .base_url("https://example.test///")
        .build()
        .unwrap();
    assert_eq!(fb.base_url(), "https://example.test");
    assert_eq!(FiloBook::new().unwrap().base_url(), DEFAULT_BASE_URL);
}
