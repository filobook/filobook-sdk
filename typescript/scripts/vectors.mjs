// Writes spec/vectors/*.json from this reference implementation (run `pnpm build` first).
// Every SDK in this repo runs these files in its tests.
import { writeFileSync } from "node:fs";
import { canonicalJson, chainHash, fillPayloadHash, formatMicro, GENESIS_HASH, parsePrice, signWebhook } from "../dist/index.js";

const out = (name, data) => writeFileSync(new URL(`../../spec/vectors/${name}.json`, import.meta.url), `${JSON.stringify(data, null, "\t")}\n`);

// ---------- prices ----------
const parseInputs = ["0.0042", "1", "1.5", " 2.25 ", "0.000001", "1000000", "999999.999999", "0", "0.0", "0.0000001", "1000000.000001", "10000000", "-1", "+1", "1e-3", "1.", ".5", "", "abc", "1,5", "0.10", "00012.340000"];
const formatInputs = [0, 1, 10, 4200, 1_000_000, 1_500_000, 1_234_567, 999_999_999_999, 1_000_000_000_000, -2500, -1_000_000];
out("prices", {
	description: "parse: decimal USDG string -> micro-USDG integer (null = rejected). format: micro-USDG integer -> shortest exact decimal string.",
	parse: parseInputs.map((input) => ({ input, micro: parsePrice(input) })),
	format: formatInputs.map((micro) => ({ micro, text: formatMicro(micro) })),
});

// ---------- canonical JSON ----------
const canon = [
	{ name: "flat object, keys sorted", input: { b: 1, a: "x", c: true } },
	{ name: "nested objects and arrays", input: { z: [3, 1, { y: null, x: false }], a: { d: [], c: {} } } },
	{ name: "array order kept", input: [3, "b", "a", 1] },
	{ name: "scalars", input: ["", 0, -42, 9007199254740991, true, false, null] },
	{ name: "escapes", input: { s: "quote \" backslash \\ slash / tab \t newline \n cr \r bs \b ff \f" } },
	{ name: "control characters", input: { s: "\u0000\u0001\u001f\u007f" } },
	{ name: "non-ASCII is not escaped", input: { "é": "café ✓ 日本 😀   " } },
	{ name: "keys sort by UTF-16 code units", input: { "": 1, "😀": 2, "z": 3, "Z": 4, "é": 5, "aa": 6, "a": 7 } },
	{ name: "fill payload shape", input: { saved: "0.01", price: "0.04", id: "fil_0000000000000001", created_at: "2026-01-01T00:00:00.000Z" } },
];
out("canonical_json", {
	description: "input (a JSON value) -> canonical JSON string: keys sorted by UTF-16 code units at every level, no whitespace, strings escaped exactly like JavaScript JSON.stringify, integers only.",
	cases: canon.map((c) => ({ ...c, output: canonicalJson(c.input) })),
});

// ---------- ledger ----------
const base = [
	{ id: "fil_000000000001", order_id: "ord_0000000000000001", agent: "0x1111111111111111111111111111111111111111", provider_id: "prv_aaaaaaaaaaaa", provider_name: "Example Provider A", max_price: "0.05", price: "0.032", saved: "0.018", created_at: "2026-01-01T00:00:00.000Z" },
	{ id: "fil_000000000002", order_id: "ord_0000000000000002", agent: "0x2222222222222222222222222222222222222222", provider_id: "prv_bbbbbbbbbbbb", provider_name: "Example Provider B", max_price: "1", price: "0.75", saved: "0.25", created_at: "2026-01-01T00:01:00.000Z" },
	{ id: "fil_000000000003", order_id: "ord_0000000000000003", agent: "0x1111111111111111111111111111111111111111", provider_id: "prv_aaaaaaaaaaaa", provider_name: "Ëxample “Provider” A", max_price: "0.000002", price: "0.000001", saved: "0.000001", created_at: "2026-01-01T00:02:00.000Z" },
];
let prev = GENESIS_HASH;
const entries = [];
const fills = [];
for (const [i, f] of base.entries()) {
	const payload_hash = await fillPayloadHash(f);
	const hash = await chainHash(prev, payload_hash);
	entries.push({ height: i + 1, kind: "fill", ref: f.id, payload_hash, prev_hash: prev, hash, created_at: f.created_at });
	fills.push({ ...f, ledger_height: i + 1, ledger_hash: hash });
	prev = hash;
}
const clone = (x) => JSON.parse(JSON.stringify(x));
const tamper = (name, fn, broken_at, reason, good = broken_at - 1) => {
	const e = clone(entries);
	const f = clone(fills);
	fn(e, f);
	return { name, entries: e, fills: f, expect: { ok: false, height: good, head: good === 0 ? GENESIS_HASH : entries[good - 1].hash, broken_at, reason } };
};
out("ledger", {
	description:
		"Each case: entries (oldest first, starting from genesis) and the fills they point to. Walk the entries in order and stop at the first failure. Checks per entry, in this order: gap (height != previous height + 1), prev_hash (!= previous hash; genesis = 64 zeros), hash (!= sha256(prev_hash + payload_hash)), missing_fill (no fill with id == ref), fill_mismatch (fill.ledger_height != height or fill.ledger_hash != hash), payload_hash (!= sha256(canonical_json(fill payload))). On failure, height/head are the last good position.",
	genesis: GENESIS_HASH,
	payload_fields: ["id", "order_id", "agent", "provider_id", "provider_name", "max_price", "price", "saved", "created_at"],
	payloads: base.map((f, i) => ({ fill: f, canonical: canonicalJson(f), payload_hash: entries[i].payload_hash })),
	cases: [
		{ name: "valid chain", entries, fills, expect: { ok: true, height: 3, head: prev } },
		{ name: "empty ledger", entries: [], fills: [], expect: { ok: true, height: 0, head: GENESIS_HASH } },
		tamper("price edited in fill 2", (e, f) => (f[1].price = "0.70"), 2, "payload_hash"),
		tamper("provider renamed in fill 1", (e, f) => (f[0].provider_name = "Someone Else"), 1, "payload_hash"),
		tamper("entry 2 removed", (e, f) => { e.splice(1, 1); f.splice(1, 1); }, 3, "gap", 1),
		tamper("entry 3 points at the wrong previous hash", (e) => (e[2].prev_hash = e[0].hash), 3, "prev_hash"),
		tamper("entry 2 hash replaced", (e) => (e[1].hash = "f".repeat(64)), 2, "hash"),
		tamper("fill 3 missing", (e, f) => f.splice(2, 1), 3, "missing_fill"),
		tamper("fill 1 claims another height", (e, f) => (f[0].ledger_height = 7), 1, "fill_mismatch"),
	],
});

// ---------- webhook ----------
const secret = "whsec_000000000000000000000000000000000000000000000000";
const body = JSON.stringify({ event: "orders.created", sent_at: "2026-01-01T00:00:00.000Z", orders: [{ id: "ord_0000000000000001", type: "web_search", max_price: "0.05", request: "Top 10 results for “x402”" }] });
const ts = "1767225600";
const sig = await signWebhook(secret, ts, body);
const testBody = JSON.stringify({ event: "test", sent_at: "2026-01-01T00:00:00.000Z", orders: [] });
out("webhook", {
	description:
		"signature = \"sha256=\" + lowercase hex(HMAC-SHA256(key = secret as UTF-8, message = timestamp + \".\" + raw body)). Verify: timestamp must be digits, signature must match ^sha256=[0-9a-f]{64}$ (else malformed); |now - timestamp| > tolerance_s -> stale_timestamp; signature mismatch (constant-time compare) -> bad_signature; then the body must parse as JSON (else malformed).",
	secret,
	sign: [
		{ timestamp: ts, body, signature: sig },
		{ timestamp: ts, body: testBody, signature: await signWebhook(secret, ts, testBody) },
	],
	verify: [
		{ name: "valid", timestamp: ts, body, signature: sig, now: 1767225600, tolerance_s: 300, ok: true },
		{ name: "valid at edge of window", timestamp: ts, body, signature: sig, now: 1767225900, tolerance_s: 300, ok: true },
		{ name: "too old", timestamp: ts, body, signature: sig, now: 1767225901, tolerance_s: 300, ok: false, error: "stale_timestamp" },
		{ name: "from the future", timestamp: ts, body, signature: sig, now: 1767225299, tolerance_s: 300, ok: false, error: "stale_timestamp" },
		{ name: "body changed", timestamp: ts, body: body.replace("0.05", "0.06"), signature: sig, now: 1767225600, tolerance_s: 300, ok: false, error: "bad_signature" },
		{ name: "timestamp changed", timestamp: "1767225601", body, signature: sig, now: 1767225600, tolerance_s: 300, ok: false, error: "bad_signature" },
		{ name: "wrong secret", timestamp: ts, body, signature: await signWebhook("whsec_other", ts, body), now: 1767225600, tolerance_s: 300, ok: false, error: "bad_signature" },
		{ name: "missing prefix", timestamp: ts, body, signature: sig.slice(7), now: 1767225600, tolerance_s: 300, ok: false, error: "malformed" },
		{ name: "uppercase hex", timestamp: ts, body, signature: `sha256=${sig.slice(7).toUpperCase()}`, now: 1767225600, tolerance_s: 300, ok: false, error: "malformed" },
		{ name: "missing timestamp", timestamp: "", body, signature: sig, now: 1767225600, tolerance_s: 300, ok: false, error: "malformed" },
		{ name: "non-numeric timestamp", timestamp: "12:00", body, signature: sig, now: 1767225600, tolerance_s: 300, ok: false, error: "malformed" },
	],
});
console.log("vectors written");
