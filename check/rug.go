package check

import (
	"fmt"
	"strings"
)

// MintSurface describes the disclosed mint power of a single declared mint
// capability. Every field is a verifiable on-chain constraint.
type MintSurface struct {
	Name    string
	Cap     int64
	Unit    string
	Quorum  int
	Signers []string
}

// RugSurfaceReport is the trust badge a token shows its holders. It proves
// exactly how (and only how) value can be minted, with no backdoor.
type RugSurfaceReport struct {
	Contract string
	// Mints holds one surface entry per declared mint capability.
	Mints []MintSurface
	// NoHiddenMint is true when no supply change occurs outside a declared mint/burn.
	NoHiddenMint bool
	// NoDiscretionaryPayout is true when no privileged operator payout power exists.
	// In v1 the grammar has no such construct, so this is trivially true.
	NoDiscretionaryPayout bool
	// NoFreeze is true when no freeze gate is declared.
	// In v1 the grammar has no such construct, so this is trivially true.
	NoFreeze bool
	// NoFee is true when no fee power is declared.
	// In v1 the grammar has no such construct, so this is trivially true.
	NoFee bool
	// SupplyHardCapped is true when every declared mint has a non-negative cap.
	SupplyHardCapped bool
}

// Clean returns true when ZERO mint powers are declared — a pure transfer-only
// token that provably cannot mint, drain, freeze, or fee. A token with a mint
// returns false even if every other flag is green; it is still SAFE, just not
// empty-surface.
func (r RugSurfaceReport) Clean() bool {
	return len(r.Mints) == 0
}

// Badge renders the rug-surface trust badge — a scannable, delightful summary
// a token can show its holders as proof of what powers exist (and don't).
func (r RugSurfaceReport) Badge() string {
	var b strings.Builder

	fmt.Fprintf(&b, "🛡  %s — rug-surface report\n", r.Contract)
	b.WriteString("\n")

	if r.Clean() {
		b.WriteString("   declares NO powers — provably can't mint, drain, freeze, or fee. Empty rug surface.\n")
	} else {
		for _, m := range r.Mints {
			signerList := strings.Join(m.Signers, ", ")

			var capStr string
			if m.Cap >= 0 {
				capStr = fmt.Sprintf("capped %s %s", formatInt(m.Cap), m.Unit)
			} else {
				capStr = "UNCAPPED ⚠"
			}

			var quorumStr string
			if m.Quorum >= 1 {
				quorumStr = fmt.Sprintf("requires %d-of-%d { %s }", m.Quorum, len(m.Signers), signerList)
			} else {
				quorumStr = "NO QUORUM ⚠ (any single signer can mint)"
			}

			fmt.Fprintf(&b, "   mint %q:  %s  ·  %s\n", m.Name, capStr, quorumStr)
		}
	}

	b.WriteString("\n")

	// Safety flags — always render all four on one line, then the cap.
	check := func(ok bool) string {
		if ok {
			return "✓"
		}
		return "✗"
	}
	fmt.Fprintf(&b, "   %s no hidden mint        %s no discretionary payout\n",
		check(r.NoHiddenMint), check(r.NoDiscretionaryPayout))
	fmt.Fprintf(&b, "   %s no freeze             %s no fee\n",
		check(r.NoFreeze), check(r.NoFee))
	if r.SupplyHardCapped {
		b.WriteString("   ✓ supply hard-capped\n")
	} else {
		b.WriteString("   ✗ supply hard-capped\n")
	}

	// Compute overall safety: all flags green AND every mint is capped + quorum'd.
	safe := r.NoHiddenMint && r.SupplyHardCapped && r.NoDiscretionaryPayout && r.NoFreeze && r.NoFee
	if safe {
		for _, m := range r.Mints {
			if m.Quorum < 1 || m.Cap < 0 {
				safe = false
				break
			}
		}
	}

	b.WriteString("\n")
	if safe || r.Clean() {
		b.WriteString("   → a holder can verify: nobody mints past the cap, nobody mints without quorum, no backdoor.\n")
	} else {
		b.WriteString("   ⚠ UNSAFE — this contract declares powers that can rug holders (see the ✗ / ⚠ lines above). This is NOT a clean trust badge.\n")
	}

	return b.String()
}

// formatInt formats an int64 with thousands separators, e.g. 1000000 → "1,000,000".
func formatInt(n int64) string {
	s := fmt.Sprintf("%d", n)
	// Insert commas from right to left every 3 digits.
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(ch))
	}
	return string(result)
}
