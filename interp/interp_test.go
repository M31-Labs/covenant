package interp_test

import (
	"os"
	"testing"

	"m31labs.dev/covenant/check"
	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/grammar"
	"m31labs.dev/covenant/interp"
	"m31labs.dev/covenant/ir"
	"m31labs.dev/covenant/state"
)

// loadContract parses and lowers community_token.cov.
func loadContract(t *testing.T) *ir.Contract {
	t.Helper()
	src, err := os.ReadFile("../examples/community_token.cov")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	tree, err := grammar.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, diags := ir.Lower(tree, src)
	if len(diags) != 0 {
		t.Fatalf("Lower diags: %v", diags)
	}
	return c
}

func loadCheckedContract(t *testing.T, path string) *ir.Contract {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	tree, err := grammar.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	c, diags := ir.Lower(tree, src)
	for _, d := range diags {
		if d.Severity == diag.Error {
			t.Fatalf("lower error: %s", d.Teach())
		}
	}
	checkDiags, _ := check.Check(c)
	for _, d := range checkDiags {
		if d.Severity == diag.Error {
			t.Fatalf("check error: %s", d.Teach())
		}
	}
	return c
}

// TestMintWithinCap: issue 500_000 with quorum satisfied and cap not hit.
func TestMintWithinCap(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	r := interp.Invoke(c, st, "issue",
		map[string]any{
			"recipient": "alice",
			"amount":    int64(500_000),
		},
		interp.Context{
			Caller:    "founder",
			Now:       1,
			Approvals: []string{"founder", "treasurer"},
		},
	)

	if !r.OK {
		t.Fatalf("expected OK, got Reason=%q", r.Reason)
	}
	if st.Supply != 500_000 {
		t.Errorf("Supply=%d, want 500_000", st.Supply)
	}
	if st.Balances["alice"] != 500_000 {
		t.Errorf("alice=%d, want 500_000", st.Balances["alice"])
	}
	if r.Hash == "" {
		t.Error("Hash must be non-empty")
	}
}

// TestMintOverCapRejectedAtomically: second mint that would exceed the 1M cap
// must be rejected and leave state unchanged.
func TestMintOverCapRejectedAtomically(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	// First mint: 500_000 — should succeed.
	r1 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(500_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
	)
	if !r1.OK {
		t.Fatalf("first mint failed unexpectedly: %q", r1.Reason)
	}

	// Second mint: 600_000 — total 1_100_000 > cap 1_000_000.
	r2 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "bob", "amount": int64(600_000)},
		interp.Context{Caller: "founder", Now: 2, Approvals: []string{"founder", "treasurer"}},
	)
	if r2.OK {
		t.Fatal("expected rejection for over-cap mint, got OK")
	}
	if r2.Reason != "MINT_CAP_EXCEEDED" {
		t.Errorf("Reason=%q, want MINT_CAP_EXCEEDED", r2.Reason)
	}
	// State must be UNCHANGED.
	if st.Supply != 500_000 {
		t.Errorf("Supply=%d after rejection, want 500_000 (unchanged)", st.Supply)
	}
	if st.Balances["bob"] != 0 {
		t.Errorf("bob balance=%d after rejection, want 0", st.Balances["bob"])
	}
}

// TestMintQuorumUnmet: only one approver when quorum is 2.
func TestMintQuorumUnmet(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	r := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(100_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder"}},
	)
	if r.OK {
		t.Fatal("expected quorum rejection, got OK")
	}
	if r.Reason != "MINT_QUORUM_UNMET" {
		t.Errorf("Reason=%q, want MINT_QUORUM_UNMET", r.Reason)
	}
	if st.Supply != 0 {
		t.Errorf("Supply=%d after rejection, want 0", st.Supply)
	}
}

// TestTransferConservesAtRuntime: move tokens from alice to bob, supply unchanged.
func TestTransferConservesAtRuntime(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	// Mint 500_000 to alice first.
	r1 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(500_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
	)
	if !r1.OK {
		t.Fatalf("setup mint failed: %q", r1.Reason)
	}

	// Transfer 200_000 from alice to bob.
	r2 := interp.Invoke(c, st, "transfer",
		map[string]any{"to": "bob", "amount": int64(200_000)},
		interp.Context{Caller: "alice", Now: 2},
	)
	if !r2.OK {
		t.Fatalf("transfer failed: %q", r2.Reason)
	}
	if st.Balances["alice"] != 300_000 {
		t.Errorf("alice=%d, want 300_000", st.Balances["alice"])
	}
	if st.Balances["bob"] != 200_000 {
		t.Errorf("bob=%d, want 200_000", st.Balances["bob"])
	}
	if st.Supply != 500_000 {
		t.Errorf("Supply=%d, want 500_000 (unchanged)", st.Supply)
	}
}

// TestTransferInsufficientBalance: alice has 0 tokens, transfer should fail.
func TestTransferInsufficientBalance(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	r := interp.Invoke(c, st, "transfer",
		map[string]any{"to": "bob", "amount": int64(200_000)},
		interp.Context{Caller: "alice", Now: 1},
	)
	if r.OK {
		t.Fatal("expected insufficient-balance rejection, got OK")
	}
	if r.Reason != "INSUFFICIENT_BALANCE" {
		t.Errorf("Reason=%q, want INSUFFICIENT_BALANCE", r.Reason)
	}
	// State must be unchanged.
	if st.Supply != 0 {
		t.Errorf("Supply=%d after rejection, want 0", st.Supply)
	}
	if st.Balances["alice"] != 0 {
		t.Errorf("alice=%d after rejection, want 0", st.Balances["alice"])
	}
}

// TestQuorumDedupBypassRejected: a single signer repeated twice must NOT satisfy
// a quorum of 2. The dedup bug allows "founder","founder" to count as 2 — this
// test must FAIL on the buggy code and PASS after the fix.
func TestQuorumDedupBypassRejected(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	r := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(100_000)},
		interp.Context{
			Caller:    "founder",
			Now:       1,
			Approvals: []string{"founder", "founder"}, // same signer twice
		},
	)
	if r.OK {
		t.Fatal("expected MINT_QUORUM_UNMET: duplicate signer must not count twice")
	}
	if r.Reason != "MINT_QUORUM_UNMET" {
		t.Errorf("Reason=%q, want MINT_QUORUM_UNMET", r.Reason)
	}
	if st.Supply != 0 {
		t.Errorf("Supply=%d after rejection, want 0 (state unchanged)", st.Supply)
	}
}

// TestQuorumDistinctSignersAccepted: two DISTINCT valid signers must satisfy
// a quorum of 2. Guards against an over-aggressive fix breaking the happy path.
func TestQuorumDistinctSignersAccepted(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	r := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(250_000)},
		interp.Context{
			Caller:    "founder",
			Now:       1,
			Approvals: []string{"founder", "treasurer"}, // two distinct signers
		},
	)
	if !r.OK {
		t.Fatalf("expected OK for two distinct signers, got Reason=%q", r.Reason)
	}
	if st.Supply != 250_000 {
		t.Errorf("Supply=%d, want 250_000", st.Supply)
	}
}

// TestBurnPath: mint 500_000 to alice then burn 200_000 via retire.
// Supply and balance must both decrease; conservation must hold.
func TestBurnPath(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	// Setup: mint 500_000 to alice.
	r1 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(500_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
	)
	if !r1.OK {
		t.Fatalf("setup mint failed: %q", r1.Reason)
	}

	// Burn 200_000 from alice.
	r2 := interp.Invoke(c, st, "retire",
		map[string]any{"amount": int64(200_000)},
		interp.Context{Caller: "alice", Now: 3},
	)
	if !r2.OK {
		t.Fatalf("retire failed: %q", r2.Reason)
	}
	if st.Balances["alice"] != 300_000 {
		t.Errorf("alice=%d after burn, want 300_000", st.Balances["alice"])
	}
	if st.Supply != 300_000 {
		t.Errorf("Supply=%d after burn, want 300_000", st.Supply)
	}
	// Conservation: sum(balances) == supply.
	var sumBal int64
	for _, v := range st.Balances {
		sumBal += v
	}
	if sumBal != st.Supply {
		t.Errorf("conservation violated: sum(balances)=%d != Supply=%d", sumBal, st.Supply)
	}
}

// TestNegativeBurnRejected: retire with a negative amount must be rejected and
// leave state UNCHANGED. Before the fix this minted 1M tokens — CRITICAL bug.
func TestNegativeBurnRejected(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	r := interp.Invoke(c, st, "retire",
		map[string]any{"amount": int64(-1_000_000)},
		interp.Context{Caller: "alice", Now: 1},
	)
	if r.OK {
		t.Fatal("expected rejection for negative amount, got OK (critical: this minted tokens from nothing)")
	}
	if r.Reason != "NEGATIVE_AMOUNT" {
		t.Errorf("Reason=%q, want NEGATIVE_AMOUNT", r.Reason)
	}
	if st.Supply != 0 {
		t.Errorf("Supply=%d after rejection, want 0 (state must be unchanged)", st.Supply)
	}
	if st.Balances["alice"] != 0 {
		t.Errorf("alice=%d after rejection, want 0 (state must be unchanged)", st.Balances["alice"])
	}
}

// TestNegativeTransferRejected: transfer with a negative amount must be rejected.
// Before the fix a negative transfer would drain the recipient and credit the caller.
func TestNegativeTransferRejected(t *testing.T) {
	c := loadContract(t)
	st := state.New()

	// Setup: mint 500_000 to alice.
	r1 := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(500_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
	)
	if !r1.OK {
		t.Fatalf("setup mint failed: %q", r1.Reason)
	}

	// Negative transfer from alice to bob — must be rejected.
	r2 := interp.Invoke(c, st, "transfer",
		map[string]any{"to": "bob", "amount": int64(-100_000)},
		interp.Context{Caller: "alice", Now: 2},
	)
	if r2.OK {
		t.Fatal("expected rejection for negative transfer amount, got OK")
	}
	if r2.Reason != "NEGATIVE_AMOUNT" {
		t.Errorf("Reason=%q, want NEGATIVE_AMOUNT", r2.Reason)
	}
	// Balances must be unchanged.
	if st.Balances["alice"] != 500_000 {
		t.Errorf("alice=%d after rejection, want 500_000 (unchanged)", st.Balances["alice"])
	}
	if st.Balances["bob"] != 0 {
		t.Errorf("bob=%d after rejection, want 0 (unchanged)", st.Balances["bob"])
	}
}

// TestMintOverflowRejected: a mint body that credits `amount` to two recipients
// sums to 2×amount. With a large amount the sum overflows int64 to a negative
// value, which (before the fix) slips under the cap check and is committed —
// minting ~1e19 tokens against a 1e6 cap. The runtime must reject it with
// MINT_OVERFLOW and leave state untouched.
func TestMintOverflowRejected(t *testing.T) {
	c := &ir.Contract{
		Name:       "TwoCredit",
		Ledgers:    []ir.Ledger{{Name: "balances", Unit: "TOK", Line: 1}},
		Supply:     &ir.Supply{Name: "total", Unit: "TOK", Line: 2},
		Invariants: []ir.Invariant{{Kind: "conserves", Ledger: "balances", Supply: "total", Line: 3}},
		Mints: []ir.Mint{{
			Name: "issue", Cap: 1_000_000, Unit: "TOK", Quorum: 2,
			Signers: []string{"a", "b"}, Line: 4,
			Body: []ir.Op{
				{Kind: ir.OpCredit, Account: "r1", Amount: "amount", Line: 5},
				{Kind: ir.OpCredit, Account: "r2", Amount: "amount", Line: 6},
			},
		}},
	}
	st := state.New()
	r := interp.Invoke(c, st, "issue",
		map[string]any{"r1": "alice", "r2": "bob", "amount": int64(5_000_000_000_000_000_000)},
		interp.Context{Caller: "a", Now: 1, Approvals: []string{"a", "b"}},
	)
	if r.OK {
		t.Fatalf("overflowing mint must be rejected, got OK with supply=%d", st.Supply)
	}
	if r.Reason != "MINT_OVERFLOW" {
		t.Errorf("Reason=%q, want MINT_OVERFLOW", r.Reason)
	}
	if st.Supply != 0 || len(st.Balances) != 0 {
		t.Errorf("state must be untouched after rejection: supply=%d balances=%v", st.Supply, st.Balances)
	}
}

// TestReceiptDeterministic: same inputs → identical Hash.
func TestReceiptDeterministic(t *testing.T) {
	c := loadContract(t)

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
	if r1.Hash != r2.Hash {
		t.Errorf("hashes differ: %q vs %q", r1.Hash, r2.Hash)
	}
	if r1.Hash == "" {
		t.Error("Hash must not be empty")
	}
}

func TestPolicyMintTimelockAndRate(t *testing.T) {
	c := loadCheckedContract(t, "../examples/policy_token.cov")
	st := state.New()

	beforeTimelock := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(50_000)},
		interp.Context{Caller: "founder", Now: 1, Approvals: []string{"founder", "treasurer"}},
	)
	if beforeTimelock.OK || beforeTimelock.Reason != "MINT_TIMELOCK_PENDING" {
		t.Fatalf("want MINT_TIMELOCK_PENDING, got OK=%v Reason=%q", beforeTimelock.OK, beforeTimelock.Reason)
	}

	afterTimelock := int64(7 * 24 * 60 * 60)
	first := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "alice", "amount": int64(60_000)},
		interp.Context{Caller: "founder", Now: afterTimelock, Approvals: []string{"founder", "treasurer"}},
	)
	if !first.OK {
		t.Fatalf("first mint should pass, got %q", first.Reason)
	}

	overRate := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "bob", "amount": int64(50_000)},
		interp.Context{Caller: "founder", Now: afterTimelock + 1, Approvals: []string{"founder", "treasurer"}},
	)
	if overRate.OK || overRate.Reason != "MINT_RATE_EXCEEDED" {
		t.Fatalf("want MINT_RATE_EXCEEDED, got OK=%v Reason=%q", overRate.OK, overRate.Reason)
	}
	if st.Balances["bob"] != 0 || st.Supply != 60_000 {
		t.Fatalf("over-rate rejection must leave state unchanged: supply=%d bob=%d", st.Supply, st.Balances["bob"])
	}

	nextWindow := interp.Invoke(c, st, "issue",
		map[string]any{"recipient": "bob", "amount": int64(50_000)},
		interp.Context{Caller: "founder", Now: afterTimelock + 30*24*60*60, Approvals: []string{"founder", "treasurer"}},
	)
	if !nextWindow.OK {
		t.Fatalf("next-window mint should pass, got %q", nextWindow.Reason)
	}
	if st.Supply != 110_000 {
		t.Fatalf("supply=%d, want 110000", st.Supply)
	}
}
