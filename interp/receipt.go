package interp

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// Delta records the net balance change for a single account.
type Delta struct {
	Account string
	Amount  int64 // positive = gained, negative = lost
}

// Receipt is the deterministic audit record emitted by every Invoke call.
type Receipt struct {
	Contract    string
	Action      string
	Caller      string
	Now         int64
	OK          bool
	Reason      string  // "" on success; reject code otherwise
	Deltas      []Delta // sorted by Account for determinism
	SupplyDelta int64
	Hash        string // sha256 hex over canonical encoding
}

// computeHash builds a deterministic sha256 hex digest over the receipt
// fields. Deltas must already be sorted by Account before calling.
func computeHash(r *Receipt) string {
	var b strings.Builder
	b.WriteString(r.Contract)
	b.WriteByte('|')
	b.WriteString(r.Action)
	b.WriteByte('|')
	b.WriteString(r.Caller)
	b.WriteByte('|')
	fmt.Fprintf(&b, "%d", r.Now)
	b.WriteByte('|')
	if r.OK {
		b.WriteString("OK")
	} else {
		b.WriteString("FAIL")
	}
	b.WriteByte('|')
	b.WriteString(r.Reason)
	b.WriteByte('|')
	// Deltas are already sorted by Account; iterate deterministically.
	for _, d := range r.Deltas {
		b.WriteString(d.Account)
		b.WriteByte(':')
		fmt.Fprintf(&b, "%d", d.Amount)
		b.WriteByte(';')
	}
	b.WriteByte('|')
	fmt.Fprintf(&b, "%d", r.SupplyDelta)

	sum := sha256.Sum256([]byte(b.String()))
	return fmt.Sprintf("%x", sum)
}

// sortDeltas sorts a slice of Delta by Account lexicographically.
func sortDeltas(ds []Delta) {
	sort.Slice(ds, func(i, j int) bool {
		return ds[i].Account < ds[j].Account
	})
}
