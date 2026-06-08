package check

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/grammar"
	"m31labs.dev/covenant/ir"
)

// TestFlagshipClean parses the community_token.cov example, runs Check, and
// asserts the full set of rug-safety invariants that make a "clean" report.
func TestFlagshipClean(t *testing.T) {
	src, err := os.ReadFile("../examples/community_token.cov")
	if err != nil {
		t.Fatalf("read community_token.cov: %v", err)
	}
	tree, err := grammar.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, lowerDiags := ir.Lower(tree, src)
	for _, d := range lowerDiags {
		if d.Severity == diag.Error {
			t.Fatalf("lower error: %s", d.Teach())
		}
	}

	diagnostics, report := Check(c)

	// No errors expected.
	for _, d := range diagnostics {
		if d.Severity == diag.Error {
			t.Errorf("unexpected error diagnostic: %s", d.Teach())
		}
	}

	// Mint surface assertions.
	if len(report.Mints) != 1 {
		t.Fatalf("want 1 mint surface, got %d", len(report.Mints))
	}
	m := report.Mints[0]
	if m.Cap != 1_000_000 {
		t.Errorf("want Cap=1000000, got %d", m.Cap)
	}
	if m.Quorum != 2 {
		t.Errorf("want Quorum=2, got %d", m.Quorum)
	}
	if len(m.Signers) != 3 {
		t.Errorf("want 3 signers, got %d: %v", len(m.Signers), m.Signers)
	}

	// Rug-safety flags.
	if !report.NoHiddenMint {
		t.Error("want NoHiddenMint=true")
	}
	if !report.NoFreeze {
		t.Error("want NoFreeze=true")
	}
	if !report.NoDiscretionaryPayout {
		t.Error("want NoDiscretionaryPayout=true")
	}
	if !report.SupplyHardCapped {
		t.Error("want SupplyHardCapped=true")
	}

	// A token with a declared mint is NOT clean.
	if report.Clean() {
		t.Error("want Clean()=false (contract has a mint)")
	}

	// Badge must contain thousands-formatted cap and quorum notation.
	badge := report.Badge()
	if !strings.Contains(badge, "1,000,000") {
		t.Errorf("badge missing thousands-formatted cap; badge:\n%s", badge)
	}
	if !strings.Contains(badge, "2-of-3") {
		t.Errorf("badge missing 2-of-3 quorum notation; badge:\n%s", badge)
	}
}

// TestUncappedMintRejected checks that a mint with Cap==-1 produces MINT_UNCAPPED.
func TestUncappedMintRejected(t *testing.T) {
	c := &ir.Contract{
		Name: "BadToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Mints: []ir.Mint{
			{Name: "issue", Cap: -1, Unit: "TOK", Quorum: 2, Signers: []string{"alice", "bob"}, Line: 4},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "MINT_UNCAPPED" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want MINT_UNCAPPED diagnostic, got: %v", diagnostics)
	}
}

// TestNoQuorumRejected checks that a mint with Quorum==0 produces MINT_NO_QUORUM.
func TestNoQuorumRejected(t *testing.T) {
	c := &ir.Contract{
		Name: "BadToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Mints: []ir.Mint{
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 0, Signers: []string{"alice", "bob"}, Line: 4},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "MINT_NO_QUORUM" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want MINT_NO_QUORUM diagnostic, got: %v", diagnostics)
	}
}

// TestBadgeNeverLies asserts two things:
//  1. A safe (flagship) badge still contains the reassuring footer, the
//     thousands-formatted cap, and the N-of-M quorum notation.
//  2. An UNSAFE badge (Quorum==0 and/or Cap==-1) does NOT contain the
//     reassuring footer, does NOT contain "requires 0-of" or "capped -1",
//     and DOES contain a warning marker (UNSAFE or ⚠).
func TestBadgeNeverLies(t *testing.T) {
	// --- positive case: safe flagship contract ---
	safeContract := &ir.Contract{
		Name: "SafeToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Mints: []ir.Mint{
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 2, Signers: []string{"alice", "bob", "carol"}, Line: 4},
		},
	}
	_, safeReport := Check(safeContract)
	safeBadge := safeReport.Badge()

	if !strings.Contains(safeBadge, "a holder can verify") {
		t.Errorf("safe badge must contain 'a holder can verify'; badge:\n%s", safeBadge)
	}
	if !strings.Contains(safeBadge, "2-of-3") {
		t.Errorf("safe badge must contain '2-of-3'; badge:\n%s", safeBadge)
	}
	if !strings.Contains(safeBadge, "1,000,000") {
		t.Errorf("safe badge must contain '1,000,000'; badge:\n%s", safeBadge)
	}

	// --- negative case: unsafe contract (Quorum==0, Cap==-1) ---
	unsafeContract := &ir.Contract{
		Name: "UnsafeToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Mints: []ir.Mint{
			{Name: "issue", Cap: -1, Unit: "TOK", Quorum: 0, Signers: []string{"alice", "bob", "carol"}, Line: 4},
		},
	}
	_, unsafeReport := Check(unsafeContract)
	unsafeBadge := unsafeReport.Badge()

	if strings.Contains(unsafeBadge, "a holder can verify") {
		t.Errorf("unsafe badge must NOT contain 'a holder can verify'; badge:\n%s", unsafeBadge)
	}
	if strings.Contains(unsafeBadge, "requires 0-of") {
		t.Errorf("unsafe badge must NOT contain 'requires 0-of'; badge:\n%s", unsafeBadge)
	}
	if strings.Contains(unsafeBadge, "capped -1") {
		t.Errorf("unsafe badge must NOT contain 'capped -1'; badge:\n%s", unsafeBadge)
	}
	if !strings.Contains(unsafeBadge, "UNSAFE") && !strings.Contains(unsafeBadge, "⚠") {
		t.Errorf("unsafe badge must contain a warning marker (UNSAFE or ⚠); badge:\n%s", unsafeBadge)
	}
}

// TestUnconservedValue checks that a Supply not referenced by any conserves
// invariant produces UNCONSERVED_VALUE.
func TestUnconservedValue(t *testing.T) {
	c := &ir.Contract{
		Name: "BadToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		// Supply present but no invariant covers it.
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		// No Invariants — coverage gap.
		Mints: []ir.Mint{
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 2, Signers: []string{"alice", "bob"}, Line: 4},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "UNCONSERVED_VALUE" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want UNCONSERVED_VALUE diagnostic, got: %v", diagnostics)
	}
}
