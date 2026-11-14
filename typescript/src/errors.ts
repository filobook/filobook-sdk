/** Any non-2xx answer from the API. `code` is the API's machine-readable error, e.g. "rate_limited". */
export class FiloBookError extends Error {
	readonly status: number;
	readonly code: string;
	constructor(status: number, code: string, message: string) {
		super(message);
		this.name = "FiloBookError";
		this.status = status;
		this.code = code;
	}
}

export type WebhookErrorCode = "malformed" | "stale_timestamp" | "bad_signature";

/** A feed webhook that failed verification. Answer it with a 4xx and do not act on it. */
export class WebhookError extends Error {
	readonly code: WebhookErrorCode;
	constructor(code: WebhookErrorCode, message: string) {
		super(message);
		this.name = "WebhookError";
		this.code = code;
	}
}
