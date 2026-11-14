package filobook

// Recompute the fill ledger: every link, every hash, and every payload hash from its fill.

// BreakReason says which check a ledger entry failed.
type BreakReason string

// Ledger break reasons, in the order they are checked.
const (
	BreakGap          BreakReason = "gap"
	BreakPrevHash     BreakReason = "prev_hash"
	BreakHash         BreakReason = "hash"
	BreakMissingFill  BreakReason = "missing_fill"
	BreakFillMismatch BreakReason = "fill_mismatch"
	BreakPayloadHash  BreakReason = "payload_hash"
)

// ChainPosition is a point in the ledger: the last height and its hash.
type ChainPosition struct {
	Height int64
	Head   string
}

// Genesis is the position before the first entry.
var Genesis = ChainPosition{Height: 0, Head: GenesisHash}

// ChainResult is the outcome of a ledger walk. Height and Head are the last good position; when OK is false,
// BrokenAt is the failing entry's height and Reason the check it failed.
type ChainResult struct {
	OK       bool        `json:"ok"`
	Height   int64       `json:"height"`
	Head     string      `json:"head"`
	BrokenAt int64       `json:"broken_at,omitempty"`
	Reason   BreakReason `json:"reason,omitempty"`
}

// VerifyEntries checks entries (oldest first, continuing from `from`, usually Genesis) against their fills,
// keyed by fill id. A nil or absent fill counts as missing. It stops at the first broken entry.
func VerifyEntries(entries []LedgerEntry, fills map[string]*Fill, from ChainPosition) ChainResult {
	height, head := from.Height, from.Head
	for _, e := range entries {
		broken := func(r BreakReason) ChainResult {
			return ChainResult{OK: false, Height: height, Head: head, BrokenAt: e.Height, Reason: r}
		}
		if e.Height != height+1 {
			return broken(BreakGap)
		}
		if e.PrevHash != head {
			return broken(BreakPrevHash)
		}
		if e.Hash != ChainHash(e.PrevHash, e.PayloadHash) {
			return broken(BreakHash)
		}
		fill := fills[e.Ref]
		if fill == nil {
			return broken(BreakMissingFill)
		}
		if fill.ID != e.Ref || fill.LedgerHeight != e.Height || fill.LedgerHash != e.Hash {
			return broken(BreakFillMismatch)
		}
		if ph, err := FillPayloadHash(fill); err != nil || ph != e.PayloadHash {
			return broken(BreakPayloadHash)
		}
		height, head = e.Height, e.Hash
	}
	return ChainResult{OK: true, Height: height, Head: head}
}
