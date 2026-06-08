// Package covenant_test is the end-to-end acceptance gate for the Covenant
// language thesis: "unrug-pullable by default". It exercises the full pipeline
// (parse → lower → check → interpret) and locks the thesis as a regression gate.
package covenant_test

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/covenant/check"
	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/grammar"
	"m31labs.dev/covenant/interp"
	"m31labs.dev/covenant/ir"
	"m31labs.dev/covenant/state"
)

// parseAndLower is a helper that parses a .cov file and lowers it to IR.
// It returns the contract (may be nil on syntax error) and all diagnostics.
func parseAndLower(t *testing.T, path string) (*ir.Contract, []diag.Diagnostic) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	tree, err := grammar.Parse(src)
	if err != nil {
		t.Fatalf("Parse %s: %v", path, err)
	}
	c, ds := ir.Lower(tree, src)
	return c, ds
}

// hasCode returns true when any diagnostic in ds matches the given code.
func hasCode(ds []diag.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

// TestThesis_StaticGuarantees asserts the full static portion of the thesis:
//
//  1. The flagship community_token.cov is clean and safe.
//  2. The hidden-mint fixture is rejected with CONSERVE_UNMINTED.
//  3. IR-level uncapped / no-quorum / unconserved contracts are rejected.
func TestThesis_StaticGuarantees(t *testing.T) {
	// -------------------------------------------------------------------------
	// 1. Flagship: parse+lower must produce no Error diagnostics; report must
	//    reflect the exact declared parameters; badge must contain the expected
	//    strings that prove the contract is auditable.
	// -------------------------------------------------------------------------
	t.Run("flagship_clean_and_safe", func(t *testing.T) {
		c, lowerDiags := parseAndLower(t, "examples/community_token.cov")

		for _, d := range lowerDiags {
			if d.Severity == diag.Error {
				t.Fatalf("unexpected lower Error: %s", d.Teach())
			}
		}

		checkDiags, report := check.Check(c)
		for _, d := range checkDiags {
			if d.Severity == diag.Error {
				t.Errorf("unexpected check Error: %s", d.Teach())
			}
		}

		// Mint surface
		if len(report.Mints) != 1 {
			t.Fatalf("want 1 mint surface, got %d", len(report.Mints))
		}
		m := report.Mints[0]
		if m.Cap != 1_000_000 {
			t.Errorf("Cap=%d, want 1_000_000", m.Cap)
		}
		if m.Quorum != 2 {
			t.Errorf("Quorum=%d, want 2", m.Quorum)
		}
		if len(m.Signers) != 3 {
			t.Errorf("len(Signers)=%d, want 3 (got %v)", len(m.Signers), m.Signers)
		}

		// Rug-safety flags
		if !report.NoHiddenMint {
			t.Error("NoHiddenMint must be true for the flagship")
		}
		if !report.SupplyHardCapped {
			t.Error("SupplyHardCapped must be true for the flagship")
		}

		// Badge must contain the verifiable human-readable guarantees
		badge := report.Badge()
		if !strings.Contains(badge, "1,000,000") {
			t.Errorf("badge missing thousands-formatted cap\n%s", badge)
		}
		if !strings.Contains(badge, "2-of-3") {
			t.Errorf("badge missing 2-of-3 quorum notation\n%s", badge)
		}
		if !strings.Contains(badge, "a holder can verify") {
			t.Errorf("badge missing holder-verify footer\n%s", badge)
		}
	})

	// -------------------------------------------------------------------------
	// 2. Hidden-mint fixture: the grammar allows a transition containing
	//    `credit` (valid syntax), but Check must detect the supply delta and
	//    reject it with CONSERVE_UNMINTED — the primary rug-pull vector.
	// -------------------------------------------------------------------------
	t.Run("hidden_mint_rejected", func(t *testing.T) {
		c, lowerDiags := parseAndLower(t, "examples/_bad_hidden_mint.cov")

		// The file must parse cleanly — no PARSE_SYNTAX_ERROR from lower.
		if hasCode(lowerDiags, "PARSE_SYNTAX_ERROR") {
			t.Fatal("_bad_hidden_mint.cov must parse without syntax errors; " +
				"a transition with `credit` is valid syntax")
		}

		checkDiags, _ := check.Check(c)
		if !hasCode(checkDiags, "CONSERVE_UNMINTED") {
			t.Errorf("want CONSERVE_UNMINTED; got: %v", checkDiags)
		}
	})

	t.Run("source_uncapped_mint_rejected", func(t *testing.T) {
		c, lowerDiags := parseAndLower(t, "examples/_bad_uncapped_mint.cov")

		if hasCode(lowerDiags, "PARSE_SYNTAX_ERROR") {
			t.Fatal("_bad_uncapped_mint.cov must parse so Check can teach MINT_UNCAPPED")
		}

		checkDiags, report := check.Check(c)
		if !hasCode(checkDiags, "MINT_UNCAPPED") {
			t.Errorf("want MINT_UNCAPPED; got: %v", checkDiags)
		}
		if report.SupplyHardCapped {
			t.Error("uncapped mint must not report SupplyHardCapped")
		}
	})

	t.Run("net_zero_drain_rejected", func(t *testing.T) {
		c, lowerDiags := parseAndLower(t, "examples/_bad_net_zero_drain.cov")

		if hasCode(lowerDiags, "PARSE_SYNTAX_ERROR") {
			t.Fatal("_bad_net_zero_drain.cov must parse so Check can reject the capability shape")
		}

		checkDiags, _ := check.Check(c)
		if !hasCode(checkDiags, "TRANSITION_SUPPLY_OP") {
			t.Errorf("want TRANSITION_SUPPLY_OP; got: %v", checkDiags)
		}
	})

	// -------------------------------------------------------------------------
	// 3. IR-level rejections: contracts that can't be expressed syntactically
	//    (uncapped mint, no-quorum mint, unconserved supply) are still rejected
	//    by Check when constructed directly — the static checker is the last
	//    line of defence even against generated or hand-crafted IR.
	// -------------------------------------------------------------------------
	t.Run("uncapped_mint_rejected", func(t *testing.T) {
		c := &ir.Contract{
			Name:    "UncappedToken",
			Ledgers: []ir.Ledger{{Name: "balances", Unit: "TOK", Line: 1}},
			Supply:  &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
			Invariants: []ir.Invariant{
				{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
			},
			Mints: []ir.Mint{
				{Name: "issue", Cap: -1, Unit: "TOK", Quorum: 2, Signers: []string{"a", "b"}, Line: 4},
			},
		}
		ds, _ := check.Check(c)
		if !hasCode(ds, "MINT_UNCAPPED") {
			t.Errorf("want MINT_UNCAPPED; got: %v", ds)
		}
	})

	t.Run("no_quorum_mint_rejected", func(t *testing.T) {
		c := &ir.Contract{
			Name:    "NoQuorumToken",
			Ledgers: []ir.Ledger{{Name: "balances", Unit: "TOK", Line: 1}},
			Supply:  &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
			Invariants: []ir.Invariant{
				{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
			},
			Mints: []ir.Mint{
				{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 0, Signers: []string{"a", "b"}, Line: 4},
			},
		}
		ds, _ := check.Check(c)
		if !hasCode(ds, "MINT_NO_QUORUM") {
			t.Errorf("want MINT_NO_QUORUM; got: %v", ds)
		}
	})

	t.Run("unconserved_supply_rejected", func(t *testing.T) {
		// A supply with no matching conserves invariant must produce UNCONSERVED_VALUE.
		c := &ir.Contract{
			Name:    "UnconservedToken",
			Ledgers: []ir.Ledger{{Name: "balances", Unit: "TOK", Line: 1}},
			Supply:  &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
			// No Invariants: no conserves(balances, total)
			Mints: []ir.Mint{
				{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 2, Signers: []string{"a", "b"}, Line: 4},
			},
		}
		ds, _ := check.Check(c)
		if !hasCode(ds, "UNCONSERVED_VALUE") {
			t.Errorf("want UNCONSERVED_VALUE; got: %v", ds)
		}
	})
}

// TestThesis_RuntimeGuarantees exercises the interpreter against the flagship
// contract on a fresh state, asserting the key runtime invariants:
//
//   - Mint within cap succeeds; supply matches minted amount.
//   - A second mint that would exceed the cap is rejected atomically; supply
//     stays unchanged.
//   - A transfer conserves supply: alice − amount, bob + amount, total stable.
//   - A mint with only one approver (quorum=2) is rejected.
func TestThesis_RuntimeGuarantees(t *testing.T) {
	c, lowerDiags := parseAndLower(t, "examples/community_token.cov")
	for _, d := range lowerDiags {
		if d.Severity == diag.Error {
			t.Fatalf("setup: lower error: %s", d.Teach())
		}
	}

	st := state.New()

	// --- Mint 500_000 with quorum satisfied ---
	r1 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(500_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
	)
	if !r1.OK {
		t.Fatalf("first mint: expected OK, got Reason=%q", r1.Reason)
	}
	if st.Supply != 500_000 {
		t.Errorf("after mint: Supply=%d, want 500_000", st.Supply)
	}

	// --- Second mint of 600_000 would push total to 1_100_000 > cap 1_000_000 ---
	r2 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "bob", "amount": int64(600_000)},
		interp.Context{Caller: "founder", Now: 2, Approvals: []string{"founder", "treasurer"}},
	)
	if r2.OK {
		t.Fatal("over-cap mint: expected rejection, got OK")
	}
	if r2.Reason != "MINT_CAP_EXCEEDED" {
		t.Errorf("over-cap mint: Reason=%q, want MINT_CAP_EXCEEDED", r2.Reason)
	}
	// State must be UNCHANGED (atomicity guarantee)
	if st.Supply != 500_000 {
		t.Errorf("after cap rejection: Supply=%d, want 500_000 (unchanged)", st.Supply)
	}

	// --- Transfer 200_000 alice → bob ---
	r3 := interp.Invoke(c, st, "transfer",
		map[string]any{"to": "bob", "amount": int64(200_000)},
		interp.Context{Caller: "alice", Now: 3},
	)
	if !r3.OK {
		t.Fatalf("transfer: expected OK, got Reason=%q", r3.Reason)
	}
	if st.Balances["alice"] != 300_000 {
		t.Errorf("alice=%d, want 300_000", st.Balances["alice"])
	}
	if st.Balances["bob"] != 200_000 {
		t.Errorf("bob=%d, want 200_000", st.Balances["bob"])
	}
	if st.Supply != 500_000 {
		t.Errorf("after transfer: Supply=%d, want 500_000 (conserved)", st.Supply)
	}

	// --- Mint with only one approver (quorum unmet) ---
	r4 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "carol", "amount": int64(100_000)},
		interp.Context{Caller: "founder", Now: 4, Approvals: []string{"founder"}},
	)
	if r4.OK {
		t.Fatal("quorum-unmet mint: expected rejection, got OK")
	}
	if r4.Reason != "MINT_QUORUM_UNMET" {
		t.Errorf("quorum-unmet mint: Reason=%q, want MINT_QUORUM_UNMET", r4.Reason)
	}
}

// TestThesis_Determinism asserts that invoking the same successful action on
// an identical fresh state twice produces an identical Receipt.Hash. This
// guarantees the receipt is a stable audit record — any two auditors running
// the same inputs will agree on the hash.
func TestThesis_Determinism(t *testing.T) {
	c, lowerDiags := parseAndLower(t, "examples/community_token.cov")
	for _, d := range lowerDiags {
		if d.Severity == diag.Error {
			t.Fatalf("setup: lower error: %s", d.Teach())
		}
	}

	invoke := func() interp.Receipt {
		st := state.New()
		return interp.Invoke(c, st, "issue",
			map[string]any{"recipient": "alice", "amount": int64(500_000)},
			interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
		)
	}

	r1 := invoke()
	r2 := invoke()

	if !r1.OK || !r2.OK {
		t.Fatalf("invocations failed: %q / %q", r1.Reason, r2.Reason)
	}
	if r1.Hash == "" {
		t.Error("Hash must be non-empty")
	}
	if r1.Hash != r2.Hash {
		t.Errorf("determinism violated: hash1=%q hash2=%q", r1.Hash, r2.Hash)
	}
}
