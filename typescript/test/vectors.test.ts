import { describe, expect, it } from "vitest";
import canonical from "../../spec/vectors/canonical_json.json";
import ledger from "../../spec/vectors/ledger.json";
import prices from "../../spec/vectors/prices.json";
import webhook from "../../spec/vectors/webhook.json";
import { GENESIS_HASH, canonicalJson, fillPayloadHash, formatMicro, parsePrice, signWebhook, verifyEntries, verifyWebhook, WebhookError, type Fill, type LedgerEntry } from "../src";

describe("prices", () => {
	it.each(prices.parse)("parse $input", ({ input, micro }) => expect(parsePrice(input)).toBe(micro));
	it.each(prices.format)("format $micro", ({ micro, text }) => expect(formatMicro(micro)).toBe(text));
});

describe("canonical json", () => {
	it.each(canonical.cases)("$name", ({ input, output }) => expect(canonicalJson(input)).toBe(output));
});

describe("ledger", () => {
	it("genesis", () => expect(ledger.genesis).toBe(GENESIS_HASH));
	it.each(ledger.payloads)("payload $fill.id", async ({ fill, canonical: c, payload_hash }) => {
		expect(canonicalJson(fill)).toBe(c);
		expect(await fillPayloadHash(fill)).toBe(payload_hash);
	});
	it.each(ledger.cases)("$name", async ({ entries, fills, expect: want }) => {
		const byId = Object.fromEntries((fills as Fill[]).map((f) => [f.id, f]));
		expect(await verifyEntries(entries as LedgerEntry[], byId)).toEqual(want);
	});
});

describe("webhook", () => {
	it.each(webhook.sign)("sign $timestamp", async ({ timestamp, body, signature }) => expect(await signWebhook(webhook.secret, timestamp, body)).toBe(signature));
	it.each(webhook.verify)("verify: $name", async (c) => {
		const run = verifyWebhook({ secret: webhook.secret, body: c.body, timestamp: c.timestamp, signature: c.signature, now: c.now, toleranceS: c.tolerance_s });
		if (c.ok) await expect(run).resolves.toEqual(JSON.parse(c.body));
		else await expect(run).rejects.toMatchObject({ code: c.error });
	});
	it("accepts bytes", async () => {
		const c = webhook.verify[0];
		await expect(verifyWebhook({ secret: webhook.secret, body: new TextEncoder().encode(c.body), timestamp: c.timestamp, signature: c.signature, now: c.now })).resolves.toBeTruthy();
	});
	it("throws WebhookError", async () => {
		await expect(verifyWebhook({ secret: "x", body: "{}", timestamp: null, signature: null })).rejects.toBeInstanceOf(WebhookError);
	});
});
