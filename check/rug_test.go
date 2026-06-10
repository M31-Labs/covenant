package check

import (
	"encoding/json"
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

func TestRugSurfaceProofJSONAndHash(t *testing.T) {
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
	_, report := Check(c)

	proofJSON, err := report.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var proof RugSurfaceProof
	if err := json.Unmarshal([]byte(proofJSON), &proof); err != nil {
		t.Fatalf("proof JSON must parse: %v\n%s", err, proofJSON)
	}
	if proof.Schema != "m31labs.covenant.rug_surface.v1" {
		t.Fatalf("schema=%q", proof.Schema)
	}
	if !proof.Safe {
		t.Fatal("flagship proof should be safe")
	}
	if proof.EmptyRugSurface {
		t.Fatal("flagship has a disclosed mint, so the surface is not empty")
	}
	if len(proof.Mints) != 1 || proof.Mints[0].Cap != 1_000_000 || proof.Mints[0].Quorum != 2 {
		t.Fatalf("bad mint proof: %+v", proof.Mints)
	}

	h1, err := report.ProofHash()
	if err != nil {
		t.Fatalf("ProofHash: %v", err)
	}
	h2, err := report.ProofHash()
	if err != nil {
		t.Fatalf("ProofHash second call: %v", err)
	}
	if h1 == "" || h1 != h2 {
		t.Fatalf("proof hash must be stable and non-empty: %q / %q", h1, h2)
	}
}

func TestPolicyMintSurfaceDisclosesSchedule(t *testing.T) {
	src, err := os.ReadFile("../examples/policy_token.cov")
	if err != nil {
		t.Fatalf("read policy_token.cov: %v", err)
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
	for _, d := range diagnostics {
		if d.Severity == diag.Error {
			t.Fatalf("unexpected check error: %s", d.Teach())
		}
	}
	if len(report.Mints) != 1 {
		t.Fatalf("mints=%+v", report.Mints)
	}
	m := report.Mints[0]
	if m.Policy != "council" {
		t.Fatalf("policy=%q", m.Policy)
	}
	if m.Quorum != 2 || len(m.Signers) != 3 {
		t.Fatalf("resolved authority missing: %+v", m)
	}
	if m.TimelockSeconds != 7*24*60*60 {
		t.Fatalf("timelock=%d", m.TimelockSeconds)
	}
	if m.Rate == nil || m.Rate.Amount != 100_000 || m.Rate.WindowSeconds != 30*24*60*60 {
		t.Fatalf("rate=%+v", m.Rate)
	}
	badge := report.Badge()
	if !strings.Contains(badge, "policy council") || !strings.Contains(badge, "timelock 7 days") || !strings.Contains(badge, "rate 100,000 TOKEN per 30 days") {
		t.Fatalf("badge missing policy/rate/timelock:\n%s", badge)
	}
}

func TestUnknownPolicyRejected(t *testing.T) {
	c := &ir.Contract{
		Name: "MissingPolicy",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Mints: []ir.Mint{
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Policy: "council", Body: []ir.Op{{Kind: ir.OpCredit, Account: "recipient", Amount: "amount", Line: 4}}, Line: 4},
		},
	}

	diagnostics, _ := Check(c)
	found := false
	for _, d := range diagnostics {
		if d.Code == "POLICY_UNKNOWN" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Fatalf("want POLICY_UNKNOWN, got %v", diagnostics)
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

func TestSourceUncappedMintRejected(t *testing.T) {
	src, err := os.ReadFile("../examples/_bad_uncapped_mint.cov")
	if err != nil {
		t.Fatalf("read _bad_uncapped_mint.cov: %v", err)
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

	found := false
	for _, d := range diagnostics {
		if d.Code == "MINT_UNCAPPED" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want MINT_UNCAPPED diagnostic, got: %v", diagnostics)
	}
	if report.SupplyHardCapped {
		t.Error("uncapped source mint must not report SupplyHardCapped")
	}
	if strings.Contains(report.Badge(), "a holder can verify") {
		t.Errorf("unsafe badge must not print holder-verification footer:\n%s", report.Badge())
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

func TestNetZeroDrainRejected(t *testing.T) {
	src, err := os.ReadFile("../examples/_bad_net_zero_drain.cov")
	if err != nil {
		t.Fatalf("read _bad_net_zero_drain.cov: %v", err)
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

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "TRANSITION_SUPPLY_OP" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want TRANSITION_SUPPLY_OP diagnostic, got: %v", diagnostics)
	}
}

func TestTransitionCannotMoveFromNonCaller(t *testing.T) {
	c := &ir.Contract{
		Name: "DrainToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Transitions: []ir.Transition{
			{
				Name:      "steal",
				Authority: "caller owns amount",
				Body: []ir.Op{
					{Kind: ir.OpMove, Ledger: "balances", From: "victim", To: "caller", Amount: "amount", Line: 4},
				},
				Line: 4,
			},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "TRANSITION_NOT_CALLER_FUNDED" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want TRANSITION_NOT_CALLER_FUNDED diagnostic, got: %v", diagnostics)
	}
}

func TestUnsupportedAuthorityRejected(t *testing.T) {
	c := &ir.Contract{
		Name: "OpenToken",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Transitions: []ir.Transition{
			{
				Name:      "unguarded",
				Authority: "caller == owner",
				Body: []ir.Op{
					{Kind: ir.OpMove, Ledger: "balances", From: "caller", To: "to", Amount: "amount", Line: 4},
				},
				Line: 4,
			},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "AUTH_UNSUPPORTED" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want AUTH_UNSUPPORTED diagnostic, got: %v", diagnostics)
	}
}

func TestMintCannotDebit(t *testing.T) {
	c := &ir.Contract{
		Name: "MintDrain",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Mints: []ir.Mint{
			{
				Name:    "issue",
				Cap:     1_000_000,
				Unit:    "TOK",
				Quorum:  2,
				Signers: []string{"alice", "bob"},
				Body: []ir.Op{
					{Kind: ir.OpDebit, Account: "victim", Amount: "amount", Line: 4},
				},
				Line: 4,
			},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "MINT_NON_CREDIT_OP" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want MINT_NON_CREDIT_OP diagnostic, got: %v", diagnostics)
	}
}

func TestBurnMustDebitCaller(t *testing.T) {
	c := &ir.Contract{
		Name: "BurnDrain",
		Ledgers: []ir.Ledger{
			{Name: "balances", Unit: "TOK", Line: 1},
		},
		Supply: &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{
			{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3},
		},
		Burns: []ir.Burn{
			{
				Name:      "retire",
				Authority: "caller owns amount",
				Body: []ir.Op{
					{Kind: ir.OpDebit, Account: "victim", Amount: "amount", Line: 4},
				},
				Line: 4,
			},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "BURN_NOT_CALLER_FUNDED" && d.Severity == diag.Error {
			found = true
		}
	}
	if !found {
		t.Errorf("want BURN_NOT_CALLER_FUNDED diagnostic, got: %v", diagnostics)
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

// TestImpossibleQuorumWarned: a mint whose quorum exceeds its signer count can
// never fire. Check must emit a MINT_IMPOSSIBLE_QUORUM Warning (NOT Error).
func TestImpossibleQuorumWarned(t *testing.T) {
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
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 5, Signers: []string{"a", "b"}, Line: 4},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "MINT_IMPOSSIBLE_QUORUM" {
			if d.Severity != diag.Warning {
				t.Errorf("MINT_IMPOSSIBLE_QUORUM must be a Warning, got Severity=%d", d.Severity)
			}
			found = true
		}
		// Must not be an Error-level diagnostic for this code.
		if d.Code == "MINT_IMPOSSIBLE_QUORUM" && d.Severity == diag.Error {
			t.Error("MINT_IMPOSSIBLE_QUORUM must NOT be an Error")
		}
	}
	if !found {
		t.Errorf("want MINT_IMPOSSIBLE_QUORUM warning diagnostic, got: %v", diagnostics)
	}
}

// TestDuplicateSignersWarned: a mint whose Signers list contains duplicates
// must emit a MINT_DUPLICATE_SIGNERS Warning (NOT Error).
func TestDuplicateSignersWarned(t *testing.T) {
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
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 2, Signers: []string{"admin", "admin"}, Line: 4},
		},
	}

	diagnostics, _ := Check(c)

	found := false
	for _, d := range diagnostics {
		if d.Code == "MINT_DUPLICATE_SIGNERS" {
			if d.Severity != diag.Warning {
				t.Errorf("MINT_DUPLICATE_SIGNERS must be a Warning, got Severity=%d", d.Severity)
			}
			found = true
		}
		if d.Code == "MINT_DUPLICATE_SIGNERS" && d.Severity == diag.Error {
			t.Error("MINT_DUPLICATE_SIGNERS must NOT be an Error")
		}
	}
	if !found {
		t.Errorf("want MINT_DUPLICATE_SIGNERS warning diagnostic, got: %v", diagnostics)
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

// hasErr reports whether ds contains any Error-severity diagnostic.
func hasErr(ds []diag.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == diag.Error {
			return true
		}
	}
	return false
}

// TestBadgeUnsafeForLaneViolation locks the badge-never-lies thesis for the
// capability-lane class: a net-zero drain transition (debit victim / credit
// caller) has a perfectly safe mint surface but is rejected by Check. The
// report — text badge AND machine-readable JSON proof — must NOT claim safety.
// Before the fix, Safe() derived from the mint-surface flags alone and reported
// safe:true for a contract the compiler rejects.
func TestBadgeUnsafeForLaneViolation(t *testing.T) {
	c := &ir.Contract{
		Name:       "NetZeroDrain",
		Ledgers:    []ir.Ledger{{Name: "balances", Unit: "TOK", Line: 1}},
		Supply:     &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3}},
		Mints: []ir.Mint{
			{Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 2, Signers: []string{"a", "b"}, Line: 4},
		},
		Transitions: []ir.Transition{{
			Name: "drain", Authority: "caller owns amount", Line: 6,
			Body: []ir.Op{
				{Kind: ir.OpDebit, Account: "victim", Amount: "amount", Line: 7},
				{Kind: ir.OpCredit, Account: "caller", Amount: "amount", Line: 8},
			},
		}},
	}

	ds, report := Check(c)
	if !hasErr(ds) {
		t.Fatal("setup: drain transition must produce an Error diagnostic")
	}
	if report.Safe() {
		t.Error("report.Safe() must be false when Check produced an Error diagnostic")
	}
	if report.Proof().Safe {
		t.Error("JSON proof Safe must be false for a contract the compiler rejects")
	}
	badge := report.Badge()
	if strings.Contains(badge, "a holder can verify") {
		t.Errorf("badge must not claim safety for a rejected contract:\n%s", badge)
	}
	if !strings.Contains(badge, "UNSAFE") {
		t.Errorf("badge must render UNSAFE for a rejected contract:\n%s", badge)
	}
}

// TestBadgeUnsafeForCleanContractWithBadTransition covers the subtle Clean()
// path: a contract with NO mint (empty mint surface) but a draining transition.
// The badge must not print the "declares NO powers" safety line just because
// no mint is declared — the static checks still failed.
func TestBadgeUnsafeForCleanContractWithBadTransition(t *testing.T) {
	c := &ir.Contract{
		Name:       "CleanButDrains",
		Ledgers:    []ir.Ledger{{Name: "balances", Unit: "TOK", Line: 1}},
		Supply:     &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3}},
		Transitions: []ir.Transition{{
			Name: "drain", Authority: "caller owns amount", Line: 5,
			Body: []ir.Op{
				{Kind: ir.OpDebit, Account: "victim", Amount: "amount", Line: 6},
				{Kind: ir.OpCredit, Account: "caller", Amount: "amount", Line: 7},
			},
		}},
	}

	_, report := Check(c)
	if !report.Clean() {
		t.Fatal("setup: contract has no mint, want Clean()=true")
	}
	if report.Safe() {
		t.Error("a Clean contract with a draining transition must not be Safe()")
	}
	badge := report.Badge()
	if strings.Contains(badge, "declares NO powers") {
		t.Errorf("badge must not claim 'declares NO powers' for a draining contract:\n%s", badge)
	}
	if !strings.Contains(badge, "UNSAFE") {
		t.Errorf("badge must render UNSAFE:\n%s", badge)
	}
}
