package check

import (
	"fmt"
	"strings"

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
	ds = append(ds, resolveMintPolicies(c)...)

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
	ds = append(ds, checkCapabilityShapes(c)...)

	// --- 2. Mint-safety checks ---

	for _, m := range c.Mints {
		if d := checkMintCap(m); d != nil {
			ds = append(ds, *d)
		}
		if d := checkMintQuorum(m); d != nil {
			ds = append(ds, *d)
		}
		if d := checkMintImpossibleQuorum(m); d != nil {
			ds = append(ds, *d)
		}
		if d := checkMintDuplicateSigners(m); d != nil {
			ds = append(ds, *d)
		}
		if d := checkMintRate(m); d != nil {
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
			Name:            m.Name,
			Cap:             m.Cap,
			Unit:            m.Unit,
			Quorum:          m.Quorum,
			Signers:         signers,
			Policy:          m.Policy,
			TimelockSeconds: m.TimelockSeconds,
		})
		if m.Rate != nil {
			mints[len(mints)-1].Rate = &RateLimitSurface{
				Amount:        m.Rate.Amount,
				Unit:          m.Rate.Unit,
				WindowSeconds: m.Rate.WindowSeconds,
			}
		}
	}

	// hasError is the authoritative safety gate: the report is safe only when
	// EVERY check passed. This captures the capability-lane and
	// conservation-coverage violations that have no mint-surface flag.
	hasError := false
	for _, d := range ds {
		if d.Severity == diag.Error {
			hasError = true
			break
		}
	}

	report := RugSurfaceReport{
		Contract:              c.Name,
		Mints:                 mints,
		NoHiddenMint:          noHiddenMint,
		NoDiscretionaryPayout: true, // no such power in v1 grammar
		NoFreeze:              true, // no such power in v1 grammar
		NoFee:                 true, // no such power in v1 grammar
		SupplyHardCapped:      supplyCapped,
		hasError:              hasError,
	}

	return ds, report
}

func resolveMintPolicies(c *ir.Contract) []diag.Diagnostic {
	var ds []diag.Diagnostic
	policies := make(map[string]ir.Policy, len(c.Policies))
	for _, p := range c.Policies {
		if _, exists := policies[p.Name]; exists {
			ds = append(ds, diag.Diagnostic{
				Severity: diag.Error,
				Line:     p.Line,
				Code:     "POLICY_DUPLICATE",
				Message:  fmt.Sprintf("policy %q is declared more than once", p.Name),
				Why:      "duplicate policy names make authority resolution ambiguous",
				Fix:      "give each policy a unique name",
			})
			continue
		}
		policies[p.Name] = p
	}

	for i := range c.Mints {
		m := &c.Mints[i]
		if m.Policy == "" {
			continue
		}
		p, ok := policies[m.Policy]
		if !ok {
			ds = append(ds, diag.Diagnostic{
				Severity: diag.Error,
				Line:     m.Line,
				Code:     "POLICY_UNKNOWN",
				Message:  fmt.Sprintf("mint %q references unknown policy %q", m.Name, m.Policy),
				Why:      "a capability cannot rely on governance that is not declared",
				Fix:      fmt.Sprintf("declare `policy %s = approval ...` or use inline `by approval ...`", m.Policy),
			})
			continue
		}
		m.Quorum = p.Quorum
		m.Signers = append([]string(nil), p.Signers...)
		if p.TimelockSeconds > m.TimelockSeconds {
			m.TimelockSeconds = p.TimelockSeconds
		}
	}
	return ds
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

// checkMintImpossibleQuorum returns MINT_IMPOSSIBLE_QUORUM (Warning) when the
// quorum exceeds the number of declared signers — the mint can never fire.
// This is safe but deceptive: the badge's N-of-M reads as if N approvals are
// possible, when in fact the mint is permanently locked.
func checkMintImpossibleQuorum(m ir.Mint) *diag.Diagnostic {
	if m.Quorum > len(m.Signers) {
		return &diag.Diagnostic{
			Severity: diag.Warning,
			Line:     m.Line,
			Code:     "MINT_IMPOSSIBLE_QUORUM",
			Message:  fmt.Sprintf("mint %q requires %d approvals but only has %d signers — it can never fire", m.Name, m.Quorum, len(m.Signers)),
			Why:      "this mint requires more approvals than it has signers — it can never fire (safe, but the badge's N-of-M reads as deceptive)",
			Fix:      "set quorum ≤ number of signers",
		}
	}
	return nil
}

// checkMintDuplicateSigners returns MINT_DUPLICATE_SIGNERS (Warning) when the
// Signers list contains repeated entries. The runtime counts distinct signers,
// so the effective quorum is lower than the badge implies.
func checkMintDuplicateSigners(m ir.Mint) *diag.Diagnostic {
	seen := make(map[string]bool, len(m.Signers))
	for _, s := range m.Signers {
		if seen[s] {
			return &diag.Diagnostic{
				Severity: diag.Warning,
				Line:     m.Line,
				Code:     "MINT_DUPLICATE_SIGNERS",
				Message:  fmt.Sprintf("mint %q lists signer %q more than once", m.Name, s),
				Why:      "a signer is listed more than once; the runtime counts distinct signers, so the effective quorum is lower than the badge implies",
				Fix:      "list each signer once",
			}
		}
		seen[s] = true
	}
	return nil
}

func checkMintRate(m ir.Mint) *diag.Diagnostic {
	if m.Rate == nil {
		return nil
	}
	if m.Rate.Amount <= 0 {
		return &diag.Diagnostic{
			Severity: diag.Error,
			Line:     m.Line,
			Code:     "MINT_BAD_RATE",
			Message:  fmt.Sprintf("mint %q has a non-positive emission rate", m.Name),
			Why:      "a rate limit must disclose a positive amount of supply per window",
			Fix:      "use `rate 10_000 TOKEN per 30 days` with a positive amount and window",
		}
	}
	if m.Rate.WindowSeconds <= 0 {
		return &diag.Diagnostic{
			Severity: diag.Error,
			Line:     m.Line,
			Code:     "MINT_BAD_RATE",
			Message:  fmt.Sprintf("mint %q has a non-positive emission window", m.Name),
			Why:      "a rate limit with no positive time window cannot constrain minting",
			Fix:      "use `rate 10_000 TOKEN per 30 days` with a positive amount and window",
		}
	}
	if m.Rate.Unit != "" && m.Rate.Unit != m.Unit {
		return &diag.Diagnostic{
			Severity: diag.Error,
			Line:     m.Line,
			Code:     "MINT_RATE_UNIT_MISMATCH",
			Message:  fmt.Sprintf("mint %q rate unit %q does not match mint unit %q", m.Name, m.Rate.Unit, m.Unit),
			Why:      "rate limits must constrain the same unit the mint creates",
			Fix:      fmt.Sprintf("change the rate unit to `%s`", m.Unit),
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

// checkCapabilityShapes keeps v1's dangerous primitives in their declared
// capability lanes. Net conservation is not enough: debit+credit inside a
// transition can conserve total supply while still draining another account.
func checkCapabilityShapes(c *ir.Contract) []diag.Diagnostic {
	var ds []diag.Diagnostic

	for _, tr := range c.Transitions {
		authAmount, ok := callerOwnsAmount(tr.Authority)
		if !ok {
			ds = append(ds, unsupportedAuthority("transition", tr.Name, tr.Authority, tr.Line))
		}
		for _, op := range tr.Body {
			switch op.Kind {
			case ir.OpMove:
				if op.From != "caller" {
					ds = append(ds, diag.Diagnostic{
						Severity: diag.Error,
						Line:     op.Line,
						Code:     "TRANSITION_NOT_CALLER_FUNDED",
						Message:  fmt.Sprintf("transition %q moves value from %q instead of caller", tr.Name, op.From),
						Why:      "v1 authority proves only that the caller owns the amount; moving from any other account is an unauthorized drain",
						Fix:      "move from `caller`, or wait for a future authority form that explicitly binds another source account",
					})
				}
				if ok && op.Amount != authAmount {
					ds = append(ds, authorityAmountMismatch("transition", tr.Name, op.Amount, authAmount, op.Line))
				}
			case ir.OpCredit, ir.OpDebit:
				ds = append(ds, diag.Diagnostic{
					Severity: diag.Error,
					Line:     op.Line,
					Code:     "TRANSITION_SUPPLY_OP",
					Message:  fmt.Sprintf("transition %q uses a supply-changing primitive", tr.Name),
					Why:      "`credit` and `debit` can compose into hidden mint, burn, or drain behavior even when the net supply delta is zero",
					Fix:      "use `move` in transitions; use declared `mint` or `burn` capabilities for supply changes",
				})
			}
		}
	}

	for _, m := range c.Mints {
		for _, op := range m.Body {
			if op.Kind != ir.OpCredit {
				ds = append(ds, diag.Diagnostic{
					Severity: diag.Error,
					Line:     op.Line,
					Code:     "MINT_NON_CREDIT_OP",
					Message:  fmt.Sprintf("mint %q contains a non-credit operation", m.Name),
					Why:      "a mint capability may only create newly disclosed supply; moving or debiting holder funds inside mint authority is a hidden power",
					Fix:      "keep mint bodies to `credit <recipient> : <amount>` operations",
				})
			}
		}
	}

	for _, b := range c.Burns {
		authAmount, ok := callerOwnsAmount(b.Authority)
		if !ok {
			ds = append(ds, unsupportedAuthority("burn", b.Name, b.Authority, b.Line))
		}
		for _, op := range b.Body {
			if op.Kind != ir.OpDebit {
				ds = append(ds, diag.Diagnostic{
					Severity: diag.Error,
					Line:     op.Line,
					Code:     "BURN_NON_DEBIT_OP",
					Message:  fmt.Sprintf("burn %q contains a non-debit operation", b.Name),
					Why:      "a burn capability may only destroy the caller's own value; crediting or moving funds here hides another power",
					Fix:      "keep burn bodies to `debit caller : <amount>` operations",
				})
				continue
			}
			if op.Account != "caller" {
				ds = append(ds, diag.Diagnostic{
					Severity: diag.Error,
					Line:     op.Line,
					Code:     "BURN_NOT_CALLER_FUNDED",
					Message:  fmt.Sprintf("burn %q debits %q instead of caller", b.Name, op.Account),
					Why:      "v1 burn authority proves only that the caller owns the amount; burning another account is a holder-drain power",
					Fix:      "burn with `debit caller : amount`, or wait for a future explicit delegated-burn authority",
				})
			}
			if ok && op.Amount != authAmount {
				ds = append(ds, authorityAmountMismatch("burn", b.Name, op.Amount, authAmount, op.Line))
			}
		}
	}

	return ds
}

func callerOwnsAmount(authority string) (string, bool) {
	parts := strings.Fields(authority)
	if len(parts) == 3 && parts[0] == "caller" && parts[1] == "owns" && parts[2] != "" {
		return parts[2], true
	}
	return "", false
}

func unsupportedAuthority(kind, name, authority string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Line:     line,
		Code:     "AUTH_UNSUPPORTED",
		Message:  fmt.Sprintf("%s %q uses unsupported authority %q", kind, name, authority),
		Why:      "v1 only has one sound authority form: `caller owns <amount>`; unknown forms would otherwise run as if unguarded",
		Fix:      "use `by (caller owns amount)` and bind body operations to the same amount",
	}
}

func authorityAmountMismatch(kind, name, opAmount, authAmount string, line int) diag.Diagnostic {
	return diag.Diagnostic{
		Severity: diag.Error,
		Line:     line,
		Code:     "AUTH_AMOUNT_MISMATCH",
		Message:  fmt.Sprintf("%s %q spends %q but authority proves %q", kind, name, opAmount, authAmount),
		Why:      "the authority check must prove the exact amount the operation spends",
		Fix:      fmt.Sprintf("change the operation amount to `%s`, or change the authority to match `%s`", authAmount, opAmount),
	}
}
