import { describe, expect, it } from "vitest";
import ledger from "../../spec/vectors/ledger.json";
import { FiloBook, FiloBookError, type Fill, type LedgerEntry } from "../src";

type Call = { url: URL; init: RequestInit };
function mock(handler: (url: URL, init: RequestInit) => { status?: number; body: unknown }) {
	const calls: Call[] = [];
	const fetch = (async (input: URL | string, init: RequestInit = {}) => {
		const url = new URL(String(input));
		calls.push({ url, init });
		const r = handler(url, init);
		return new Response(JSON.stringify(r.body), { status: r.status ?? 200, headers: { "content-type": "application/json" } });
	}) as typeof globalThis.fetch;
	return { calls, fetch };
}

describe("client", () => {
	it("sends the API key, JSON body and query", async () => {
		const m = mock(() => ({ status: 201, body: { id: "ord_1" } }));
		const fb = new FiloBook({ apiKey: "fb_live_test", fetch: m.fetch, baseUrl: "https://example.test/" });
		await fb.createOrder({ request: "r", type: "web_search", max_price: "0.05", quote_window_s: 30 });
		const { url, init } = m.calls[0];
		expect(url.toString()).toBe("https://example.test/v1/orders");
		expect(init.method).toBe("POST");
		const h = init.headers as Record<string, string>;
		expect(h.authorization).toBe("Bearer fb_live_test");
		expect(h["content-type"]).toBe("application/json");
		expect(JSON.parse(String(init.body))).toEqual({ request: "r", type: "web_search", max_price: "0.05", quote_window_s: 30 });
	});

	it("drops empty query params and unwraps lists", async () => {
		const m = mock(() => ({ body: { orders: [{ id: "a" }], cursor: null } }));
		const fb = new FiloBook({ fetch: m.fetch });
		expect(await fb.book({ type: "inference", limit: undefined })).toEqual([{ id: "a" }]);
		expect(m.calls[0].url.toString()).toBe("https://dapp.filobook.run/v1/book?type=inference");
		expect((m.calls[0].init.headers as Record<string, string>).authorization).toBeUndefined();
	});

	it("throws FiloBookError with the API code", async () => {
		const m = mock(() => ({ status: 429, body: { error: "rate_limited", message: "Too many requests" } }));
		const err = await new FiloBook({ fetch: m.fetch }).quote("ord_1", { price: "0.01" }).catch((e) => e);
		expect(err).toBeInstanceOf(FiloBookError);
		expect(err).toMatchObject({ status: 429, code: "rate_limited", message: "Too many requests" });
	});

	it("verifyLedger walks pages and fetches fills", async () => {
		const valid = ledger.cases[0];
		const entries = valid.entries as LedgerEntry[];
		const fills = Object.fromEntries((valid.fills as Fill[]).map((f) => [f.id, f]));
		const m = mock((url) => {
			if (url.pathname === "/v1/ledger") {
				const after = Number(url.searchParams.get("after"));
				const limit = Number(url.searchParams.get("limit"));
				return { body: { entries: entries.filter((e) => e.height > after).slice(0, limit) } };
			}
			const id = url.pathname.split("/").pop()!;
			return fills[id] ? { body: fills[id] } : { status: 404, body: { error: "not_found", message: "Fill not found" } };
		});
		const progress: number[] = [];
		const res = await new FiloBook({ fetch: m.fetch }).verifyLedger({ pageSize: 2, onProgress: (h) => progress.push(h) });
		expect(res).toEqual(valid.expect);
		expect(progress).toEqual([2, 3]);
	});

	it("verifyLedger reports a missing fill", async () => {
		const c = ledger.cases.find((x) => x.name === "fill 3 missing")!;
		const fills = Object.fromEntries((c.fills as Fill[]).map((f) => [f.id, f]));
		const m = mock((url) => {
			if (url.pathname === "/v1/ledger") return { body: { entries: Number(url.searchParams.get("after")) ? [] : c.entries } };
			const id = url.pathname.split("/").pop()!;
			return fills[id] ? { body: fills[id] } : { status: 404, body: { error: "not_found", message: "Fill not found" } };
		});
		expect(await new FiloBook({ fetch: m.fetch }).verifyLedger()).toEqual(c.expect);
	});
});
