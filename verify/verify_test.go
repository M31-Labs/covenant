package verify_test

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/covenant/check"
	"m31labs.dev/covenant/grammar"
	"m31labs.dev/covenant/ir"
	"m31labs.dev/covenant/verify"
)

// computeProof is a test helper that runs the real pipeline on a source file and
// returns the canonical proof an honest issuer would publish.
func computeProof(t *testing.T, path string) (src []byte, proof check.RugSurfaceProof, hash string) {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	tree, err := grammar.Parse(src)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	c, _ := ir.Lower(tree, src)
	_, report := check.Check(c)
	proof = report.Proof()
	h, err := report.ProofHash()
	if err != nil {
		t.Fatalf("proof hash: %v", err)
	}
	return src, proof, h
}

// TestVerifyMatchingProofIsVerified: a holder who runs the same source against the
// issuer's published proof gets VERIFIED — no trust in the issuer required.
func TestVerifyMatchingProofIsVerified(t *testing.T) {
	src, proof, hash := computeProof(t, "../examples/community_token.cov")

	res := verify.Verify(src, &proof, hash)
	if res.Status != verify.StatusVerified {
		t.Fatalf("Status=%q, want VERIFIED (mismatches=%v)", res.Status, res.Mismatches)
	}
	if !res.Safe {
		t.Error("flagship must verify as safe")
	}
	if res.ComputedProofSHA256 != hash {
		t.Errorf("ComputedProofSHA256=%q, want %q", res.ComputedProofSHA256, hash)
	}
}

// TestVerifyTamperedProofIsMismatch: a project that publishes a clean badge but a
// different (less safe) source must fail verification. Here the claimed proof
// understates the cap; the recomputed proof exposes the lie.
func TestVerifyTamperedProofIsMismatch(t *testing.T) {
	src, proof, _ := computeProof(t, "../examples/community_token.cov")

	// Tamper: claim a smaller cap than the source actually allows.
	tampered := proof
	tampered.TotalMintCap = 1 // source says 1_000_000
	if len(tampered.Mints) > 0 {
		tampered.Mints = append([]check.MintSurface(nil), tampered.Mints...)
		tampered.Mints[0].Cap = 1
	}

	res := verify.Verify(src, &tampered, "")
	if res.Status != verify.StatusMismatch {
		t.Fatalf("Status=%q, want MISMATCH", res.Status)
	}
	if len(res.Mismatches) == 0 {
		t.Error("MISMATCH must list at least one differing field")
	}
}

// TestVerifyHashMismatch: a claimed proof hash that doesn't match the recomputed
// hash is a MISMATCH even without the full proof body.
func TestVerifyHashMismatch(t *testing.T) {
	src, _, _ := computeProof(t, "../examples/community_token.cov")

	res := verify.Verify(src, nil, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if res.Status != verify.StatusMismatch {
		t.Fatalf("Status=%q, want MISMATCH for a wrong claimed hash", res.Status)
	}
}

// TestVerifyHashMatch: the anchored-hash path — a holder with only the source and
// the anchored proof_sha256 can confirm correspondence.
func TestVerifyHashMatch(t *testing.T) {
	src, _, hash := computeProof(t, "../examples/community_token.cov")

	res := verify.Verify(src, nil, hash)
	if res.Status != verify.StatusVerified {
		t.Fatalf("Status=%q, want VERIFIED for a matching anchored hash", res.Status)
	}
}

// TestVerifyUnsafeSourceSelfVerify: verifying an unsafe source with no claim
// reports UNSAFE (the source is honest but declares a rug power).
func TestVerifyUnsafeSourceSelfVerify(t *testing.T) {
	src, err := os.ReadFile("../examples/_bad_net_zero_drain.cov")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	res := verify.Verify(src, nil, "")
	if res.Status != verify.StatusUnsafe {
		t.Fatalf("Status=%q, want UNSAFE", res.Status)
	}
	if res.Safe {
		t.Error("drain source must not be Safe")
	}
}

// TestVerifyClaimedSafeButSourceUnsafeIsMismatch: the headline attack — a project
// publishes a clean "safe" badge for a source that the compiler rejects. The
// verifier must catch it.
func TestVerifyClaimedSafeButSourceUnsafeIsMismatch(t *testing.T) {
	src, err := os.ReadFile("../examples/_bad_net_zero_drain.cov")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Fabricate a clean proof claiming the drain contract is safe.
	fabricated := check.RugSurfaceProof{
		Schema:           "m31labs.covenant.rug_surface.v1",
		Contract:         "NetZeroDrain",
		Safe:             true,
		SupplyHardCapped: true,
		NoHiddenMint:     true,
	}
	res := verify.Verify(src, &fabricated, "")
	if res.Status != verify.StatusMismatch {
		t.Fatalf("Status=%q, want MISMATCH (fabricated clean badge on an unsafe source)", res.Status)
	}
}

// TestVerifySyntaxErrorIsError: an unparseable source can't be verified at all.
func TestVerifySyntaxErrorIsError(t *testing.T) {
	res := verify.Verify([]byte("this is not covenant {{{"), nil, "")
	if res.Status != verify.StatusError {
		t.Fatalf("Status=%q, want ERROR for unparseable source", res.Status)
	}
}

// TestVerifyJSON: the result serializes to stable JSON for machine consumers.
func TestVerifyJSON(t *testing.T) {
	src, proof, hash := computeProof(t, "../examples/community_token.cov")
	res := verify.Verify(src, &proof, hash)
	out, err := res.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if !strings.Contains(out, "\"status\": \"VERIFIED\"") {
		t.Errorf("JSON must contain status VERIFIED:\n%s", out)
	}
}
