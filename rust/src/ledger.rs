//! Recompute the fill ledger: every link, every hash, and every payload hash from its fill.

use std::collections::HashMap;
use std::fmt;

use serde::{Deserialize, Serialize};

use crate::protocol::{chain_hash, fill_payload_hash, GENESIS_HASH};
use crate::types::{Fill, LedgerEntry};

/// Why a ledger walk stopped. Checked per entry in this order.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BreakReason {
    /// `height != previous height + 1`.
    Gap,
    /// `prev_hash != previous hash`.
    PrevHash,
    /// `hash != sha256(prev_hash + payload_hash)`.
    Hash,
    /// No fill with `id == ref`.
    MissingFill,
    /// The fill's `id`, `ledger_height` or `ledger_hash` does not match the entry.
    FillMismatch,
    /// `payload_hash != sha256(canonical_json(fill payload))`.
    PayloadHash,
}

impl BreakReason {
    /// The wire name, e.g. `"prev_hash"`.
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Gap => "gap",
            Self::PrevHash => "prev_hash",
            Self::Hash => "hash",
            Self::MissingFill => "missing_fill",
            Self::FillMismatch => "fill_mismatch",
            Self::PayloadHash => "payload_hash",
        }
    }
}

impl fmt::Display for BreakReason {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// A position in the chain: the height and hash of the last verified entry.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ChainPosition {
    /// Height of the last verified entry (0 before the first).
    pub height: u64,
    /// Hash of the last verified entry ([`GENESIS_HASH`] before the first).
    pub head: String,
}

impl ChainPosition {
    /// Before the first entry: height 0, head [`GENESIS_HASH`].
    pub fn genesis() -> Self {
        Self {
            height: 0,
            head: GENESIS_HASH.to_owned(),
        }
    }
}

impl Default for ChainPosition {
    fn default() -> Self {
        Self::genesis()
    }
}

/// Outcome of a walk. `height`/`head` are the last good position; `broken_at` and `reason` are set when
/// `ok` is false.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ChainResult {
    /// True when every entry checked out.
    pub ok: bool,
    /// Height of the last good entry.
    pub height: u64,
    /// Hash of the last good entry.
    pub head: String,
    /// Height of the first failing entry.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub broken_at: Option<u64>,
    /// Why that entry failed.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reason: Option<BreakReason>,
}

impl ChainResult {
    /// A successful walk ending at `pos`.
    pub fn ok_at(pos: ChainPosition) -> Self {
        Self {
            ok: true,
            height: pos.height,
            head: pos.head,
            broken_at: None,
            reason: None,
        }
    }

    /// The last good position.
    pub fn position(&self) -> ChainPosition {
        ChainPosition {
            height: self.height,
            head: self.head.clone(),
        }
    }
}

/// Check entries (oldest first, starting from genesis) against their fills, keyed by fill id.
pub fn verify_entries(entries: &[LedgerEntry], fills: &HashMap<String, Fill>) -> ChainResult {
    verify_entries_from(entries, fills, ChainPosition::genesis())
}

/// Like [`verify_entries`], continuing from `from` (for walking the chain page by page). Stops at the
/// first broken entry.
pub fn verify_entries_from(
    entries: &[LedgerEntry],
    fills: &HashMap<String, Fill>,
    from: ChainPosition,
) -> ChainResult {
    let ChainPosition {
        mut height,
        mut head,
    } = from;
    for e in entries {
        let broken = |reason: BreakReason, height: u64, head: String| ChainResult {
            ok: false,
            height,
            head,
            broken_at: Some(e.height),
            reason: Some(reason),
        };
        if height.checked_add(1) != Some(e.height) {
            return broken(BreakReason::Gap, height, head);
        }
        if e.prev_hash != head {
            return broken(BreakReason::PrevHash, height, head);
        }
        if e.hash != chain_hash(&e.prev_hash, &e.payload_hash) {
            return broken(BreakReason::Hash, height, head);
        }
        let Some(fill) = fills.get(&e.fill_ref) else {
            return broken(BreakReason::MissingFill, height, head);
        };
        if fill.id != e.fill_ref || fill.ledger_height != e.height || fill.ledger_hash != e.hash {
            return broken(BreakReason::FillMismatch, height, head);
        }
        if e.payload_hash != fill_payload_hash(fill) {
            return broken(BreakReason::PayloadHash, height, head);
        }
        height = e.height;
        head.clone_from(&e.hash);
    }
    ChainResult::ok_at(ChainPosition { height, head })
}
