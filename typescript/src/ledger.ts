// Recompute the fill ledger: every link, every hash, and every payload hash from its fill.

import { GENESIS_HASH, chainHash, fillPayloadHash } from "./protocol";
import type { Fill, LedgerEntry } from "./types";

export type BreakReason = "gap" | "prev_hash" | "hash" | "missing_fill" | "fill_mismatch" | "payload_hash";

export type ChainResult =
	| { ok: true; height: number; head: string }
	| { ok: false; height: number; head: string; broken_at: number; reason: BreakReason };

export type ChainPosition = { height: number; head: string };
export const GENESIS: ChainPosition = { height: 0, head: GENESIS_HASH };

/**
 * Check entries (oldest first, continuing from `from`) against their fills. Stops at the first broken entry;
 * `height`/`head` are then the last good position.
 */
export async function verifyEntries(entries: LedgerEntry[], fills: Record<string, Fill | null | undefined>, from: ChainPosition = GENESIS): Promise<ChainResult> {
	let { height, head } = from;
	for (const e of entries) {
		const broken = (reason: BreakReason): ChainResult => ({ ok: false, height, head, broken_at: e.height, reason });
		if (e.height !== height + 1) return broken("gap");
		if (e.prev_hash !== head) return broken("prev_hash");
		if (e.hash !== (await chainHash(e.prev_hash, e.payload_hash))) return broken("hash");
		const fill = fills[e.ref];
		if (!fill) return broken("missing_fill");
		if (fill.id !== e.ref || fill.ledger_height !== e.height || fill.ledger_hash !== e.hash) return broken("fill_mismatch");
		if (e.payload_hash !== (await fillPayloadHash(fill))) return broken("payload_hash");
		height = e.height;
		head = e.hash;
	}
	return { ok: true, height, head };
}
