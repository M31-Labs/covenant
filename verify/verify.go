// Package verify provides a trustless check of a Covenant rug-surface proof: it
// recomputes the proof from source and confirms a claimed proof (or anchored
// proof hash) actually corresponds to that source — with no need to trust the
// issuer. This is what turns the rug-surface badge from a self-asserted claim
// into something a holder, wallet, explorer, or CI can independently verify.
package verify

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"m31labs.dev/covenant/check"
	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/grammar"
	"m31labs.dev/covenant/ir"
)

// Status is the verdict of a verification.
type Status string

const (
	// StatusVerified: the source produces exactly the claimed proof and is safe.
	StatusVerified Status = "VERIFIED"
	// StatusMismatch: the claimed proof/hash does NOT correspond to the source.
	StatusMismatch Status = "MISMATCH"
	// StatusUnsafe: the source is honestly described but declares a rug power.
	StatusUnsafe Status = "UNSAFE"
	// StatusError: the source could not be parsed, so nothing can be verified.
	StatusError Status = "ERROR"
)

// Result is the structured verdict of Verify.
type Result struct {
	Status              Status            `json:"status"`
	Safe                bool              `json:"safe"`
	Contract            string            `json:"contract"`
	SourceSHA256        string            `json:"source_sha256"`
	ComputedProofSHA256 string            `json:"computed_proof_sha256"`
	ClaimedProofSHA256  string            `json:"claimed_proof_sha256,omitempty"`
	Mismatches          []string          `json:"mismatches,omitempty"`
	Diagnostics         []diag.Diagnostic `json:"-"`
}

// JSON renders the result as stable indented JSON for machine consumers.
func (r Result) JSON() (string, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// Verify recomputes the rug-surface proof from src and checks it against an
// optional claimed proof and/or claimed proof hash.
//
//   - claimedProof != nil  → the full proof body the issuer published.
//   - claimedHash != ""    → just the proof_sha256 (e.g. read from an on-chain
//     anchor produced by `covenant chainplan`).
//
// Pass both nil/"" to self-verify (recompute and report SAFE/UNSAFE).
func Verify(src []byte, claimedProof *check.RugSurfaceProof, claimedHash string) Result {
	sourceSum := sha256.Sum256(src)
	res := Result{SourceSHA256: hex(sourceSum[:])}

	tree, err := grammar.Parse(src)
	if err != nil {
		res.Status = StatusError
		res.Diagnostics = []diag.Diagnostic{{
			Severity: diag.Error, Code: "VERIFY_PARSE_ERROR",
			Message: "source could not be parsed: " + err.Error(),
			Why:     "an unparseable source has no proof to verify against",
			Fix:     "check that the file is valid Covenant source",
		}}
		return res
	}

	contract, lowerDs := ir.Lower(tree, src)
	if contract == nil || hasParseError(lowerDs) {
		res.Status = StatusError
		res.Diagnostics = lowerDs
		return res
	}

	checkDs, report := check.Check(contract)
	res.Contract = report.Contract
	res.Safe = report.Safe()
	res.Diagnostics = append(lowerDs, checkDs...)

	computed := report.Proof()
	res.ComputedProofSHA256 = proofHash(computed)

	hasClaim := claimedProof != nil || claimedHash != ""
	if claimedProof != nil {
		res.ClaimedProofSHA256 = proofHash(*claimedProof)
		res.Mismatches = append(res.Mismatches, diffProofs(*claimedProof, computed)...)
	}
	if claimedHash != "" {
		res.ClaimedProofSHA256 = strings.ToLower(claimedHash)
		if !strings.EqualFold(claimedHash, res.ComputedProofSHA256) {
			res.Mismatches = append(res.Mismatches,
				fmt.Sprintf("proof hash: claimed %s, computed %s", short(claimedHash), short(res.ComputedProofSHA256)))
		}
	}

	switch {
	case hasClaim && len(res.Mismatches) > 0:
		res.Status = StatusMismatch
	case report.Safe():
		res.Status = StatusVerified
	default:
		res.Status = StatusUnsafe
	}
	return res
}

// diffProofs returns human-readable descriptions of every field where the
// claimed proof differs from the computed one.
func diffProofs(claimed, computed check.RugSurfaceProof) []string {
	var ms []string
	add := func(field string, c, w any) {
		if c != w {
			ms = append(ms, fmt.Sprintf("%s: claimed %v, computed %v", field, c, w))
		}
	}
	add("contract", claimed.Contract, computed.Contract)
	add("safe", claimed.Safe, computed.Safe)
	add("supply_hard_capped", claimed.SupplyHardCapped, computed.SupplyHardCapped)
	add("no_hidden_mint", claimed.NoHiddenMint, computed.NoHiddenMint)
	add("no_discretionary_payout", claimed.NoDiscretionaryPayout, computed.NoDiscretionaryPayout)
	add("no_freeze", claimed.NoFreeze, computed.NoFreeze)
	add("no_fee", claimed.NoFee, computed.NoFee)
	add("total_mint_cap", claimed.TotalMintCap, computed.TotalMintCap)
	add("empty_rug_surface", claimed.EmptyRugSurface, computed.EmptyRugSurface)

	if len(claimed.Mints) != len(computed.Mints) {
		ms = append(ms, fmt.Sprintf("mints: claimed %d, computed %d", len(claimed.Mints), len(computed.Mints)))
		return ms
	}
	for i := range computed.Mints {
		c, w := claimed.Mints[i], computed.Mints[i]
		if c.Name != w.Name {
			ms = append(ms, fmt.Sprintf("mint[%d].name: claimed %q, computed %q", i, c.Name, w.Name))
		}
		if c.Cap != w.Cap {
			ms = append(ms, fmt.Sprintf("mint[%d].cap: claimed %d, computed %d", i, c.Cap, w.Cap))
		}
		if c.Quorum != w.Quorum {
			ms = append(ms, fmt.Sprintf("mint[%d].quorum: claimed %d, computed %d", i, c.Quorum, w.Quorum))
		}
		if c.Unit != w.Unit {
			ms = append(ms, fmt.Sprintf("mint[%d].unit: claimed %q, computed %q", i, c.Unit, w.Unit))
		}
	}
	return ms
}

// proofHash recomputes the canonical sha256 over a proof, matching the hash an
// honest issuer's RugSurfaceReport.ProofHash() emits (sha256 over compact JSON).
func proofHash(p check.RugSurfaceProof) string {
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex(sum[:])
}

func hasParseError(ds []diag.Diagnostic) bool {
	for _, d := range ds {
		if d.Code == "PARSE_SYNTAX_ERROR" {
			return true
		}
	}
	return false
}

func hex(b []byte) string {
	return fmt.Sprintf("%x", b)
}

func short(h string) string {
	if len(h) > 16 {
		return h[:16] + "…"
	}
	return h
}
