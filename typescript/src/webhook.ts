// Feed webhooks: FiloBook POSTs new orders to a provider's webhook_url with
//   x-filobook-event: orders.created | test
//   x-filobook-timestamp: unix seconds
//   x-filobook-signature: "sha256=" + hex(HMAC-SHA256(webhook_secret, timestamp + "." + raw_body))

import { WebhookError } from "./errors";
import { hmacSha256Hex } from "./protocol";
import type { FeedEvent } from "./types";

export const SIGNATURE_HEADER = "x-filobook-signature";
export const TIMESTAMP_HEADER = "x-filobook-timestamp";
export const EVENT_HEADER = "x-filobook-event";
/** Default allowed clock difference between FiloBook and you, in seconds. */
export const DEFAULT_TOLERANCE_S = 300;

const dec = new TextDecoder();

/** The signature header value for a body. Useful for tests and for replaying a delivery yourself. */
export async function signWebhook(secret: string, timestamp: string, body: string | Uint8Array): Promise<string> {
	if (typeof body === "string") return `sha256=${await hmacSha256Hex(secret, `${timestamp}.${body}`)}`;
	const head = new TextEncoder().encode(`${timestamp}.`);
	const msg = new Uint8Array(head.length + body.length);
	msg.set(head);
	msg.set(body, head.length);
	return `sha256=${await hmacSha256Hex(secret, msg)}`;
}

function sameHex(a: string, b: string) {
	if (a.length !== b.length) return false;
	let diff = 0;
	for (let i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
	return diff === 0;
}

export type VerifyWebhookInput = {
	secret: string;
	/** The raw request body, exactly as received. Do not re-serialise parsed JSON. */
	body: string | Uint8Array;
	timestamp: string | null | undefined;
	signature: string | null | undefined;
	toleranceS?: number;
	/** Current time in unix seconds; defaults to the clock. */
	now?: number;
};

/** Check a delivery and return the parsed event. Throws WebhookError when it should be rejected. */
export async function verifyWebhook(input: VerifyWebhookInput): Promise<FeedEvent> {
	const { secret, timestamp, signature } = input;
	if (!timestamp || !/^\d{1,12}$/.test(timestamp)) throw new WebhookError("malformed", "Missing or invalid x-filobook-timestamp");
	if (!signature || !/^sha256=[0-9a-f]{64}$/.test(signature)) throw new WebhookError("malformed", "Missing or invalid x-filobook-signature");
	const now = input.now ?? Math.floor(Date.now() / 1000);
	if (Math.abs(now - Number(timestamp)) > (input.toleranceS ?? DEFAULT_TOLERANCE_S)) throw new WebhookError("stale_timestamp", "Timestamp is outside the allowed window");
	const expected = await signWebhook(secret, timestamp, input.body);
	if (!sameHex(expected, signature)) throw new WebhookError("bad_signature", "Signature does not match");
	const raw = typeof input.body === "string" ? input.body : dec.decode(input.body);
	try {
		return JSON.parse(raw) as FeedEvent;
	} catch {
		throw new WebhookError("malformed", "Body is not JSON");
	}
}

/** verifyWebhook for a Fetch API Request (Workers, Deno, Bun, Node 18+). Reads the body. */
export async function verifyWebhookRequest(req: Request, secret: string, opts: { toleranceS?: number; now?: number } = {}): Promise<FeedEvent> {
	return verifyWebhook({ secret, body: await req.text(), timestamp: req.headers.get(TIMESTAMP_HEADER), signature: req.headers.get(SIGNATURE_HEADER), ...opts });
}
