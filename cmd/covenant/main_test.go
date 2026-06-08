package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"m31labs.dev/covenant/chain"
	"m31labs.dev/covenant/check"
)

const flagship = "../../examples/community_token.cov"

// TestCheckFlagshipPasses verifies the flagship contract passes all checks.
func TestCheckFlagshipPasses(t *testing.T) {
	out, code := runCheck(flagship)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "checks pass") {
		t.Fatalf("expected 'checks pass' in output, got:\n%s", out)
	}
}

// TestRugsurfaceShowsBadge verifies the trust badge renders flagship facts.
func TestRugsurfaceShowsBadge(t *testing.T) {
	out, code := runRugsurface(flagship, false)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "1,000,000") {
		t.Fatalf("expected '1,000,000' (cap) in badge output, got:\n%s", out)
	}
	if !strings.Contains(out, "2-of-3") {
		t.Fatalf("expected '2-of-3' (quorum) in badge output, got:\n%s", out)
	}
}

func TestRugsurfaceJSONShowsProof(t *testing.T) {
	out, code := runRugsurface(flagship, true)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}

	var proof check.RugSurfaceProof
	if err := json.Unmarshal([]byte(out), &proof); err != nil {
		t.Fatalf("expected valid JSON proof: %v\n%s", err, out)
	}
	if proof.Schema != "m31labs.covenant.rug_surface.v1" {
		t.Fatalf("schema=%q", proof.Schema)
	}
	if !proof.Safe {
		t.Fatalf("expected safe proof: %+v", proof)
	}
	if len(proof.Mints) != 1 || proof.Mints[0].Cap != 1_000_000 {
		t.Fatalf("bad mint proof: %+v", proof.Mints)
	}
}

func TestRugsurfaceJSONForUnsafeIsPureJSON(t *testing.T) {
	out, code := runRugsurface("../../examples/_bad_uncapped_mint.cov", true)
	if code != 1 {
		t.Fatalf("expected exit 1 for unsafe proof, got %d\noutput:\n%s", code, out)
	}
	if strings.Contains(out, "why:") || strings.Contains(out, "fix:") {
		t.Fatalf("--json output must not include teaching text:\n%s", out)
	}

	var proof check.RugSurfaceProof
	if err := json.Unmarshal([]byte(out), &proof); err != nil {
		t.Fatalf("expected valid JSON proof despite unsafe status: %v\n%s", err, out)
	}
	if proof.Safe {
		t.Fatalf("uncapped mint proof must be unsafe: %+v", proof)
	}
	if proof.SupplyHardCapped {
		t.Fatalf("uncapped mint proof must not claim hard-capped supply: %+v", proof)
	}
}

func TestRugsurfaceJSONShowsPolicySchedule(t *testing.T) {
	out, code := runRugsurface("../../examples/policy_token.cov", true)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}

	var proof check.RugSurfaceProof
	if err := json.Unmarshal([]byte(out), &proof); err != nil {
		t.Fatalf("expected valid JSON proof: %v\n%s", err, out)
	}
	if len(proof.Mints) != 1 {
		t.Fatalf("mints=%+v", proof.Mints)
	}
	m := proof.Mints[0]
	if m.Policy != "council" {
		t.Fatalf("policy=%q", m.Policy)
	}
	if m.TimelockSeconds != 7*24*60*60 {
		t.Fatalf("timelock=%d", m.TimelockSeconds)
	}
	if m.Rate == nil || m.Rate.Amount != 100_000 || m.Rate.WindowSeconds != 30*24*60*60 {
		t.Fatalf("rate=%+v", m.Rate)
	}
}

func TestExplainPolicyToken(t *testing.T) {
	out, code := runExplain("../../examples/policy_token.cov")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"Covenant explain",
		"static checks pass",
		"policy council",
		"timelock: 7 days",
		"emission: at most 100,000 TOKEN per 30 days",
		"Result: SAFE",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestExplainUnsafeContract(t *testing.T) {
	out, code := runExplain("../../examples/_bad_uncapped_mint.cov")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"rug risk",
		"hard cap",
		"Result: UNSAFE",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestChainplanDefaultSepolia(t *testing.T) {
	out, code := runChainplan(flagship, "", false)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}
	for _, want := range []string{
		"Covenant chain plan",
		"evm-sepolia",
		"Sepolia",
		"source sha256",
		"proof  sha256",
		"SAFE to anchor",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q in output:\n%s", want, out)
		}
	}
}

func TestChainplanJSON(t *testing.T) {
	out, code := runChainplan("../../examples/policy_token.cov", "solana-devnet", true)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}

	var plan chain.Plan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("expected valid JSON: %v\n%s", err, out)
	}
	if plan.Schema != "m31labs.covenant.chain_plan.v1" {
		t.Fatalf("schema=%q", plan.Schema)
	}
	if plan.Target.ID != "solana-devnet" {
		t.Fatalf("target=%+v", plan.Target)
	}
	if plan.SourceSHA256 == "" || plan.ProofSHA256 == "" {
		t.Fatalf("missing hashes: %+v", plan)
	}
	if !plan.Safe {
		t.Fatalf("policy token should be safe: %+v", plan)
	}
}

func TestChainplanUnsafeReturnsPlanAndNonzero(t *testing.T) {
	out, code := runChainplan("../../examples/_bad_uncapped_mint.cov", "evm-sepolia", true)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, out)
	}

	var plan chain.Plan
	if err := json.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("expected valid JSON even for unsafe plan: %v\n%s", err, out)
	}
	if plan.Safe {
		t.Fatalf("unsafe contract must not produce safe chain plan: %+v", plan)
	}
}

func TestChainplanUnknownTarget(t *testing.T) {
	out, code := runChainplan(flagship, "moonbase-mainnet-ish", false)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "unknown chain target") || !strings.Contains(out, "evm-sepolia") {
		t.Fatalf("expected unknown target help, got:\n%s", out)
	}
}

// TestRunMintWithinCap verifies a successful mint invocation.
func TestRunMintWithinCap(t *testing.T) {
	out, code := runRun(
		flagship,
		"issue",
		[]string{"recipient=alice", "amount=500000"},
		"founder",
		1,
		[]string{"founder", "treasurer"},
	)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("expected 'OK' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected 'alice' in output, got:\n%s", out)
	}
}

// TestCheckOnSyntaxError verifies that a broken file exits 1 with teaching text.
func TestCheckOnSyntaxError(t *testing.T) {
	f, err := os.CreateTemp("", "broken-*.cov")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString("contract X {"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	out, code := runCheck(f.Name())
	if code != 1 {
		t.Fatalf("expected exit 1 for broken syntax, got %d\noutput:\n%s", code, out)
	}
	// The syntax error diagnostic should contain teaching text.
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "syntax") && !strings.Contains(lower, "parse") {
		t.Fatalf("expected syntax/parse teaching text in output, got:\n%s", out)
	}
}

func TestCheckOnUncappedMintTeaches(t *testing.T) {
	out, code := runCheck("../../examples/_bad_uncapped_mint.cov")
	if code != 1 {
		t.Fatalf("expected exit 1 for uncapped mint, got %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, "MINT_UNCAPPED") && !strings.Contains(strings.ToLower(out), "hard cap") {
		t.Fatalf("expected uncapped-mint teaching text, got:\n%s", out)
	}
}
