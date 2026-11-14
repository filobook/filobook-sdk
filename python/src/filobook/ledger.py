"""Recompute the fill ledger: every link, every hash, and every payload hash from its fill."""

from __future__ import annotations

from collections.abc import Iterable, Mapping
from dataclasses import dataclass
from typing import Any, Literal

from .protocol import GENESIS_HASH, chain_hash, fill_payload_hash

BreakReason = Literal["gap", "prev_hash", "hash", "missing_fill", "fill_mismatch", "payload_hash"]


@dataclass(frozen=True)
class ChainPosition:
    """A verified point in the chain: the last good height and its hash."""

    height: int
    head: str


GENESIS = ChainPosition(height=0, head=GENESIS_HASH)


@dataclass(frozen=True)
class ChainResult:
    """Result of a walk. When ``ok`` is False, ``height``/``head`` are the last good position and
    ``broken_at``/``reason`` describe the first broken entry."""

    ok: bool
    height: int
    head: str
    broken_at: int | None = None
    reason: BreakReason | None = None

    @property
    def position(self) -> ChainPosition:
        return ChainPosition(self.height, self.head)

    def to_dict(self) -> dict[str, Any]:
        """The same shape as the TypeScript SDK: broken_at and reason only when ok is False."""
        d: dict[str, Any] = {"ok": self.ok, "height": self.height, "head": self.head}
        if not self.ok:
            d["broken_at"] = self.broken_at
            d["reason"] = self.reason
        return d


def verify_entries(
    entries: Iterable[Mapping[str, Any]],
    fills: Mapping[str, Mapping[str, Any] | None],
    start: ChainPosition = GENESIS,
) -> ChainResult:
    """Check entries (oldest first, continuing from ``start``) against their fills, keyed by fill id.
    Stops at the first broken entry; ``height``/``head`` are then the last good position."""
    height, head = start.height, start.head
    for e in entries:

        def broken(reason: BreakReason) -> ChainResult:
            return ChainResult(ok=False, height=height, head=head, broken_at=e["height"], reason=reason)

        if e["height"] != height + 1:
            return broken("gap")
        if e["prev_hash"] != head:
            return broken("prev_hash")
        if e["hash"] != chain_hash(e["prev_hash"], e["payload_hash"]):
            return broken("hash")
        fill = fills.get(e["ref"])
        if not fill:
            return broken("missing_fill")
        if fill.get("id") != e["ref"] or fill.get("ledger_height") != e["height"] or fill.get("ledger_hash") != e["hash"]:
            return broken("fill_mismatch")
        if e["payload_hash"] != fill_payload_hash(fill):
            return broken("payload_hash")
        height, head = e["height"], e["hash"]
    return ChainResult(ok=True, height=height, head=head)
