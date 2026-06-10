package check

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

// MintSurface describes the disclosed mint power of a single declared mint
// capability. Every field is a verifiable on-chain constraint.
type MintSurface struct {
	Name            string            `json:"name"`
	Cap             int64             `json:"cap"`
	Unit            string            `json:"unit"`
	Quorum          int               `json:"quorum"`
	Signers         []string          `json:"signers"`
	Policy          string            `json:"policy,omitempty"`
	TimelockSeconds int64             `json:"timelock_seconds,omitempty"`
	Rate            *RateLimitSurface `json:"rate,omitempty"`
}

type RateLimitSurface struct {
	Amount        int64  `json:"amount"`
	Unit          string `json:"unit"`
	WindowSeconds int64  `json:"window_seconds"`
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
	// hasError is set by Check to true iff any Error-severity diagnostic was
	// produced. It is the authoritative safety gate: a report is safe only when
	// EVERY check passed, not just the mint-surface flags above. Without it,
	// capability-lane and conservation-coverage violations (which have no
	// corresponding flag) would render a clean badge on a rejected contract.
	hasError bool
}

// RugSurfaceProof is the stable, machine-checkable form of the trust badge.
// Its JSON is deterministic because encoding/json sorts map keys and the
// struct field order is fixed here.
type RugSurfaceProof struct {
	Schema                string        `json:"schema"`
	Contract              string        `json:"contract"`
	Mints                 []MintSurface `json:"mints"`
	NoHiddenMint          bool          `json:"no_hidden_mint"`
	NoDiscretionaryPayout bool          `json:"no_discretionary_payout"`
	NoFreeze              bool          `json:"no_freeze"`
	NoFee                 bool          `json:"no_fee"`
	SupplyHardCapped      bool          `json:"supply_hard_capped"`
	// TotalMintCap is the maximum supply mintable across ALL declared mints —
	// the holder's max-inflation number. -1 means unbounded (an uncapped mint).
	TotalMintCap int64 `json:"total_mint_cap"`
	// RecipientsRuntimeBound is true when minted-token destinations are set at
	// call time (always so in v1), so the badge cannot prove where they go.
	RecipientsRuntimeBound bool `json:"recipients_runtime_bound"`
	Safe                   bool `json:"safe"`
	EmptyRugSurface        bool `json:"empty_rug_surface"`
}

// Clean returns true when ZERO mint powers are declared — a pure transfer-only
// token that provably cannot mint, drain, freeze, or fee. A token with a mint
// returns false even if every other flag is green; it is still SAFE, just not
// empty-surface.
func (r RugSurfaceReport) Clean() bool {
	return len(r.Mints) == 0
}

// Safe returns true when the report contains no disclosed unsafe power in the
// v1 surface. Bounded, quorum-gated mints are safe but not empty-surface.
func (r RugSurfaceReport) Safe() bool {
	// Any Error diagnostic from Check (e.g. a capability-lane or
	// conservation-coverage violation) makes the contract unsafe, even when
	// every mint-surface flag is green.
	if r.hasError {
		return false
	}
	safe := r.NoHiddenMint && r.SupplyHardCapped && r.NoDiscretionaryPayout && r.NoFreeze && r.NoFee
	if safe {
		for _, m := range r.Mints {
			if m.Quorum < 1 || m.Cap < 0 {
				return false
			}
		}
	}
	return safe
}

// TotalMintCap returns the maximum supply mintable across all declared mints,
// or -1 when any mint is uncapped or the sum would overflow int64. This is the
// holder's max-inflation number: the most new supply the contract can ever create.
func (r RugSurfaceReport) TotalMintCap() int64 {
	var total int64
	for _, m := range r.Mints {
		if m.Cap < 0 {
			return -1 // an uncapped mint makes the aggregate unbounded
		}
		sum := total + m.Cap
		if sum < total { // int64 overflow
			return -1
		}
		total = sum
	}
	return total
}

// Proof returns the canonical machine-readable rug-surface proof.
func (r RugSurfaceReport) Proof() RugSurfaceProof {
	mints := make([]MintSurface, len(r.Mints))
	copy(mints, r.Mints)
	return RugSurfaceProof{
		Schema:                 "m31labs.covenant.rug_surface.v1",
		Contract:               r.Contract,
		Mints:                  mints,
		NoHiddenMint:           r.NoHiddenMint,
		NoDiscretionaryPayout:  r.NoDiscretionaryPayout,
		NoFreeze:               r.NoFreeze,
		NoFee:                  r.NoFee,
		SupplyHardCapped:       r.SupplyHardCapped,
		TotalMintCap:           r.TotalMintCap(),
		RecipientsRuntimeBound: len(r.Mints) > 0,
		Safe:                   r.Safe(),
		EmptyRugSurface:        r.Clean(),
	}
}

// JSON renders the canonical rug-surface proof as stable indented JSON.
func (r RugSurfaceReport) JSON() (string, error) {
	b, err := json.MarshalIndent(r.Proof(), "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// ProofHash returns a sha256 over the compact canonical JSON proof.
func (r RugSurfaceReport) ProofHash() (string, error) {
	b, err := json.Marshal(r.Proof())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum), nil
}

// Badge renders the rug-surface trust badge — a scannable, delightful summary
// a token can show its holders as proof of what powers exist (and don't).
func (r RugSurfaceReport) Badge() string {
	var b strings.Builder

	fmt.Fprintf(&b, "🛡  %s — rug-surface report\n", r.Contract)
	b.WriteString("\n")

	if r.Clean() {
		if r.Safe() {
			b.WriteString("   declares NO powers — provably can't mint, drain, freeze, or fee. Empty rug surface.\n")
		} else {
			b.WriteString("   no mint capability declared, but static checks failed — see the diagnostics above.\n")
		}
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
			if m.Policy != "" || m.TimelockSeconds > 0 || m.Rate != nil {
				var clauses []string
				if m.Policy != "" {
					clauses = append(clauses, fmt.Sprintf("policy %s", m.Policy))
				}
				if m.TimelockSeconds > 0 {
					clauses = append(clauses, fmt.Sprintf("timelock %s", formatDuration(m.TimelockSeconds)))
				}
				if m.Rate != nil {
					clauses = append(clauses, fmt.Sprintf("rate %s %s per %s", formatInt(m.Rate.Amount), m.Rate.Unit, formatDuration(m.Rate.WindowSeconds)))
				}
				fmt.Fprintf(&b, "      %s\n", strings.Join(clauses, "  ·  "))
			}
		}

		// Aggregate inflation ceiling across every mint — the holder's
		// max-supply number. Shown only when there is more than one mint, since
		// for a single mint it just restates the per-mint cap.
		if len(r.Mints) > 1 {
			if total := r.TotalMintCap(); total >= 0 {
				fmt.Fprintf(&b, "\n   Total mintable across all mints: %s %s\n", formatInt(total), r.Mints[0].Unit)
			} else {
				b.WriteString("\n   Total mintable across all mints: UNCAPPED ⚠\n")
			}
		}

		// Recipient disclosure: in v1 every mint credits a runtime-supplied
		// recipient, so the badge cannot prove where minted tokens land.
		b.WriteString("   note: recipient set at call time — the badge can't prove where minted tokens go\n")
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

	b.WriteString("\n")
	if r.Safe() {
		b.WriteString("   → a holder can verify: nobody mints past the cap, nobody mints without quorum, no backdoor.\n")
	} else {
		b.WriteString("   ⚠ UNSAFE — this contract declares powers that can rug holders (see the ✗ / ⚠ lines above). This is NOT a clean trust badge.\n")
	}
	if hash, err := r.ProofHash(); err == nil {
		fmt.Fprintf(&b, "   proof sha256:%s\n", hash[:16])
	}

	return b.String()
}

func formatDuration(seconds int64) string {
	switch {
	case seconds%(24*60*60) == 0:
		n := seconds / (24 * 60 * 60)
		if n == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", n)
	case seconds%(60*60) == 0:
		n := seconds / (60 * 60)
		if n == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", n)
	case seconds%60 == 0:
		n := seconds / 60
		if n == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", n)
	default:
		if seconds == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", seconds)
	}
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
