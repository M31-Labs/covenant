package check

import (
	"fmt"

	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/ir"
)

// Check runs all static checks on a lowered Contract and returns the full
// diagnostic list plus a RugSurfaceReport.
//
// Checks performed:
//  1. Conservation: every transition body must have zero net supply delta.
//     Every mint body and burn body may have a non-zero delta (that's their job).
//  2. Mint safety: every declared mint must have a hard cap (Cap >= 0) and a
//     meaningful quorum (Quorum >= 1).
//  3. Conservation coverage: every Ledger and the Supply must be referenced by
//     at least one conserves(ledger, supply) invariant.
func Check(c *ir.Contract) ([]diag.Diagnostic, RugSurfaceReport) {
	var ds []diag.Diagnostic

	// --- 1. Conservation checks ---

	for _, tr := range c.Transitions {
		if d := CheckConservation("transition", tr.Body); d != nil {
			ds = append(ds, *d)
		}
	}
	for _, m := range c.Mints {
		if d := CheckConservation("mint", m.Body); d != nil {
			ds = append(ds, *d)
		}
	}
	for _, b := range c.Burns {
		if d := CheckConservation("burn", b.Body); d != nil {
			ds = append(ds, *d)
		}
	}

	// --- 2. Mint-safety checks ---

	for _, m := range c.Mints {
		if d := checkMintCap(m); d != nil {
			ds = append(ds, *d)
		}
		if d := checkMintQuorum(m); d != nil {
			ds = append(ds, *d)
		}
	}

	// --- 3. Conservation-coverage check ---

	ds = append(ds, checkConservationCoverage(c)...)

	// --- Build RugSurfaceReport ---

	// NoHiddenMint: true iff no CONSERVE_UNMINTED diagnostic was produced.
	noHiddenMint := true
	for _, d := range ds {
		if d.Code == "CONSERVE_UNMINTED" {
			noHiddenMint = false
			break
		}
	}

	// SupplyHardCapped: true iff every declared mint has Cap >= 0.
	supplyCapped := true
	for _, m := range c.Mints {
		if m.Cap < 0 {
			supplyCapped = false
			break
		}
	}

	// Build mint surfaces.
	var mints []MintSurface
	for _, m := range c.Mints {
		signers := make([]string, len(m.Signers))
		copy(signers, m.Signers)
		mints = append(mints, MintSurface{
			Name:    m.Name,
			Cap:     m.Cap,
			Unit:    m.Unit,
			Quorum:  m.Quorum,
			Signers: signers,
		})
	}

	report := RugSurfaceReport{
		Contract:              c.Name,
		Mints:                 mints,
		NoHiddenMint:          noHiddenMint,
		NoDiscretionaryPayout: true, // no such power in v1 grammar
		NoFreeze:              true, // no such power in v1 grammar
		NoFee:                 true, // no such power in v1 grammar
		SupplyHardCapped:      supplyCapped,
	}

	return ds, report
}

// checkMintCap returns MINT_UNCAPPED when a mint has no hard cap (Cap == -1).
func checkMintCap(m ir.Mint) *diag.Diagnostic {
	if m.Cap < 0 {
		return &diag.Diagnostic{
			Severity: diag.Error,
			Line:     m.Line,
			Code:     "MINT_UNCAPPED",
			Message:  fmt.Sprintf("mint %q has no hard cap", m.Name),
			Why:      "an uncapped mint lets the operator inflate supply without limit — the classic rug",
			Fix:      "add a hard cap, e.g. `cap 1_000_000 TOKEN`",
		}
	}
	return nil
}

// checkMintQuorum returns MINT_NO_QUORUM when a mint requires no approvers.
func checkMintQuorum(m ir.Mint) *diag.Diagnostic {
	if m.Quorum < 1 {
		return &diag.Diagnostic{
			Severity: diag.Error,
			Line:     m.Line,
			Code:     "MINT_NO_QUORUM",
			Message:  fmt.Sprintf("mint %q has no quorum requirement", m.Name),
			Why:      "a single key could mint unilaterally; holders can't veto",
			Fix:      "require approval, e.g. `by approval 2 of { ... }`",
		}
	}
	return nil
}

// checkConservationCoverage ensures every Ledger and the Supply appear in at
// least one conserves(ledger, supply) invariant. Uncovered value-bearing fields
// can be moved or vanish unchecked.
func checkConservationCoverage(c *ir.Contract) []diag.Diagnostic {
	var ds []diag.Diagnostic

	// Build sets of covered ledger names and supply names from invariants.
	coveredLedgers := make(map[string]bool)
	coveredSupplies := make(map[string]bool)
	for _, inv := range c.Invariants {
		if inv.Kind == "conserves" {
			coveredLedgers[inv.Ledger] = true
			coveredSupplies[inv.Supply] = true
		}
	}

	for _, ledger := range c.Ledgers {
		if !coveredLedgers[ledger.Name] {
			ds = append(ds, diag.Diagnostic{
				Severity: diag.Error,
				Line:     ledger.Line,
				Code:     "UNCONSERVED_VALUE",
				Message:  fmt.Sprintf("ledger %q is not covered by any conservation invariant", ledger.Name),
				Why:      "value outside a conservation invariant can be moved or vanish unchecked",
				Fix:      fmt.Sprintf("add `invariant conserves(%s, <supply>)`", ledger.Name),
			})
		}
	}

	if c.Supply != nil && !coveredSupplies[c.Supply.Name] {
		ds = append(ds, diag.Diagnostic{
			Severity: diag.Error,
			Line:     c.Supply.Line,
			Code:     "UNCONSERVED_VALUE",
			Message:  fmt.Sprintf("supply %q is not covered by any conservation invariant", c.Supply.Name),
			Why:      "value outside a conservation invariant can be moved or vanish unchecked",
			Fix:      fmt.Sprintf("add `invariant conserves(<ledger>, %s)`", c.Supply.Name),
		})
	}

	return ds
}
