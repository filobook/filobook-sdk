// The parts of the protocol every SDK must compute byte-for-byte the same: price math, canonical JSON,
// the ledger hashes and the webhook signature. See spec/PROTOCOL.md; spec/vectors holds the test cases.

import type { Fill } from "./types";

/** 1 USDG = 1,000,000 micro-USDG. */
export const UNIT = 1_000_000;
/** Largest price the API accepts, in micro-USDG (1,000,000 USDG). */
export const MAX_PRICE_MICRO = 1_000_000 * UNIT;
/** prev_hash of the first ledger entry. */
export const GENESIS_HASH = "0".repeat(64);

/** "0.0042" → 4200. null unless it is a positive decimal with at most 7 integer digits and 6 decimals, ≤ 1,000,000. */
export function parsePrice(text: string): number | null {
	if (typeof text !== "string") return null;
	const m = /^(\d{1,7})(?:\.(\d{1,6}))?$/.exec(text.trim());
	if (!m) return null;
	const micro = Number(m[1]) * UNIT + Number((m[2] ?? "").padEnd(6, "0"));
	if (micro <= 0 || micro > MAX_PRICE_MICRO) return null;
	return micro;
}

/** 4200 → "0.0042". Exact; trailing zeros dropped. */
export function formatMicro(micro: number): string {
	const sign = micro < 0 ? "-" : "";
	const abs = Math.abs(Math.trunc(micro));
	const whole = Math.floor(abs / UNIT);
	const frac = String(abs % UNIT).padStart(6, "0").replace(/0+$/, "");
	return `${sign}${whole}${frac ? `.${frac}` : ""}`;
}

/**
 * JSON with object keys sorted (by UTF-16 code units) at every level and no whitespace. Strings are escaped
 * exactly like JSON.stringify. Numbers must be integers; the protocol never hashes floats.
 */
export function canonicalJson(value: unknown): string {
	if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
	if (value && typeof value === "object") {
		const obj = value as Record<string, unknown>;
		return `{${Object.keys(obj)
			.filter((k) => obj[k] !== undefined)
			.sort()
			.map((k) => `${JSON.stringify(k)}:${canonicalJson(obj[k])}`)
			.join(",")}}`;
	}
	if (typeof value === "number" && !Number.isSafeInteger(value)) throw new TypeError("canonicalJson: numbers must be safe integers");
	return JSON.stringify(value);
}

const enc = new TextEncoder();
const toHex = (buf: ArrayBuffer) => [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");

export async function sha256Hex(data: string | Uint8Array): Promise<string> {
	const bytes = typeof data === "string" ? enc.encode(data) : data;
	return toHex(await crypto.subtle.digest("SHA-256", bytes as BufferSource));
}

export async function hmacSha256Hex(secret: string, data: string | Uint8Array): Promise<string> {
	const key = await crypto.subtle.importKey("raw", enc.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
	const bytes = typeof data === "string" ? enc.encode(data) : data;
	return toHex(await crypto.subtle.sign("HMAC", key, bytes as BufferSource));
}

export type FillPayload = Pick<Fill, "id" | "order_id" | "agent" | "provider_id" | "provider_name" | "max_price" | "price" | "saved" | "created_at">;

/** The fields of a fill the ledger commits to: every field except ledger_height and ledger_hash. */
export function fillPayload(fill: FillPayload): FillPayload {
	const { id, order_id, agent, provider_id, provider_name, max_price, price, saved, created_at } = fill;
	return { id, order_id, agent, provider_id, provider_name, max_price, price, saved, created_at };
}

/** sha256(canonical_json(fill payload)), lowercase hex. */
export const fillPayloadHash = (fill: FillPayload) => sha256Hex(canonicalJson(fillPayload(fill)));

/** sha256(prev_hash + payload_hash): the two lowercase hex strings concatenated, then hashed. */
export const chainHash = (prevHash: string, payloadHash: string) => sha256Hex(prevHash + payloadHash);
