package main

import (
	"os"
	"strings"
	"testing"
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
	out, code := runRugsurface(flagship)
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
