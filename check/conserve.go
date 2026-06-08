package check

import (
	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/ir"
)

// CheckConservation enforces the supply-conservation rule for a capability body.
//
// ctx is the capability kind: "transition", "mint", or "burn".
//
// For a transition, the net supply delta must be zero — transitions move existing
// value and must never create or destroy it. A non-zero delta is a hidden mint or
// burn, the most common vector for token rugs.
//
// For mint and burn, positive and negative deltas are expected by design.
func CheckConservation(ctx string, body []ir.Op) *diag.Diagnostic {
	netDelta := 0
	firstOffendingLine := 0

	for _, op := range body {
		switch op.Kind {
		case ir.OpCredit:
			netDelta++
			if firstOffendingLine == 0 {
				firstOffendingLine = op.Line
			}
		case ir.OpDebit:
			netDelta--
			if firstOffendingLine == 0 {
				firstOffendingLine = op.Line
			}
		// OpMove: delta 0, no action needed
		}
	}

	if ctx == "transition" && netDelta != 0 {
		return &diag.Diagnostic{
			Severity: diag.Error,
			Line:     firstOffendingLine,
			Code:     "CONSERVE_UNMINTED",
			Message:  "this transition creates or destroys value",
			Why:      "a transition that changes total supply is a hidden mint/burn — the most common way tokens get rugged",
			Fix:      "use `move` to transfer existing value, or put supply changes in a declared `mint`/`burn` capability",
		}
	}

	return nil
}
