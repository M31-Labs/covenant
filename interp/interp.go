package interp

import (
	"strings"

	"m31labs.dev/covenant/ir"
	"m31labs.dev/covenant/state"
)

// Context carries the call-site context for a single Invoke.
type Context struct {
	Caller    string
	Now       int64
	Approvals []string // signers who have approved (for mint quorum)
}

// Invoke executes one action on a contract atomically.
// On success the state is updated and an OK Receipt is returned.
// On any failure the state is left UNCHANGED and a !OK Receipt is returned.
//
// The flow is: resolve → guard (authority/quorum/cap/rate) → apply on a clone →
// re-verify conservation → commit. Each stage is a focused helper so the
// orchestration reads top-to-bottom and every reject leaves state untouched.
func Invoke(c *ir.Contract, st *state.State, action string, args map[string]any, ctx Context) Receipt {
	base := Receipt{
		Contract: c.Name,
		Action:   action,
		Caller:   ctx.Caller,
		Now:      ctx.Now,
	}

	foundMint, foundTransition, foundBurn := resolveAction(c, action)
	if foundMint == nil && foundTransition == nil && foundBurn == nil {
		return reject(base, "UNKNOWN_ACTION")
	}

	// Reject any negative int64 argument up front. A negative amount passed to a
	// burn does `balance -= (-N)` = balance += N (mints from nothing, CRITICAL);
	// a negative transfer drains the recipient. Reject unconditionally.
	if reason := rejectNegativeArgs(args); reason != "" {
		return reject(base, reason)
	}

	// Guard checks BEFORE any mutation. minted is the mint's total, computed
	// once and reused for the post-apply rate-window update.
	var minted int64
	switch {
	case foundMint != nil:
		m, reason := precheckMint(foundMint, st, ctx, args)
		if reason != "" {
			return reject(base, reason)
		}
		minted = m
	case foundTransition != nil:
		if reason := checkAuthority(foundTransition.Authority, st, ctx, args); reason != "" {
			return reject(base, reason)
		}
	default:
		if reason := checkAuthority(foundBurn.Authority, st, ctx, args); reason != "" {
			return reject(base, reason)
		}
	}

	// Operate on a SNAPSHOT so any failure can be discarded.
	snap := st.Clone()
	body := capabilityBody(foundMint, foundTransition, foundBurn)
	if reason := applyBody(body, snap, ctx, args); reason != "" {
		return reject(base, reason)
	}
	if foundMint != nil && foundMint.Rate != nil {
		recordMintWindow(foundMint, snap, ctx, minted)
	}
	if reason := postCheckConservation(snap); reason != "" {
		return reject(base, reason)
	}

	// Commit: write the snapshot back into st.
	deltas := computeDeltas(st, snap)
	supplyDelta := snap.Supply - st.Supply
	st.Balances = snap.Balances
	st.Supply = snap.Supply
	st.MintWindows = snap.MintWindows

	r := base
	r.OK = true
	r.Deltas = deltas
	r.SupplyDelta = supplyDelta
	r.Hash = computeHash(&r)
	return r
}

// resolveAction finds the named capability, returning a pointer to exactly one
// of (mint, transition, burn) or all-nil when the action is unknown.
func resolveAction(c *ir.Contract, action string) (*ir.Mint, *ir.Transition, *ir.Burn) {
	for i := range c.Mints {
		if c.Mints[i].Name == action {
			return &c.Mints[i], nil, nil
		}
	}
	for i := range c.Transitions {
		if c.Transitions[i].Name == action {
			return nil, &c.Transitions[i], nil
		}
	}
	for i := range c.Burns {
		if c.Burns[i].Name == action {
			return nil, nil, &c.Burns[i]
		}
	}
	return nil, nil, nil
}

// rejectNegativeArgs returns "NEGATIVE_AMOUNT" if any int64 argument is negative.
func rejectNegativeArgs(args map[string]any) string {
	for _, v := range args {
		if n, ok := v.(int64); ok && n < 0 {
			return "NEGATIVE_AMOUNT"
		}
	}
	return ""
}

// capabilityBody returns the op body for whichever capability is non-nil.
func capabilityBody(m *ir.Mint, tr *ir.Transition, b *ir.Burn) []ir.Op {
	switch {
	case m != nil:
		return m.Body
	case tr != nil:
		return tr.Body
	default:
		return b.Body
	}
}

// precheckMint runs the mint guard chain (quorum → cap → timelock → rate) and
// returns the validated minted total, or a reject reason on the first failure.
// No state is mutated.
func precheckMint(m *ir.Mint, st *state.State, ctx Context, args map[string]any) (int64, string) {
	// Quorum: count distinct approvals that are in the signers set.
	signerSet := make(map[string]bool, len(m.Signers))
	for _, s := range m.Signers {
		signerSet[s] = true
	}
	approved := 0
	seen := make(map[string]bool)
	for _, a := range ctx.Approvals {
		if signerSet[a] && !seen[a] {
			seen[a] = true
			approved++
		}
	}
	if approved < m.Quorum {
		return 0, "MINT_QUORUM_UNMET"
	}

	// sumMinted rejects on int64 overflow so a multi-credit body cannot wrap
	// past the cap.
	minted, reason := sumMinted(m.Body, args)
	if reason != "" {
		return 0, reason
	}
	// Overflow-safe cap check: amounts and supply are ≥ 0 and supply ≤ cap, so
	// use subtraction to avoid int64 overflow.
	if m.Cap >= 0 && minted > m.Cap-st.Supply {
		return 0, "MINT_CAP_EXCEEDED"
	}
	if m.TimelockSeconds > 0 && ctx.Now < m.TimelockSeconds {
		return 0, "MINT_TIMELOCK_PENDING"
	}
	if m.Rate != nil {
		if m.Rate.WindowSeconds <= 0 || m.Rate.Amount <= 0 {
			return 0, "MINT_RATE_INVALID"
		}
		window, ok := st.MintWindows[m.Name]
		if !ok || ctx.Now >= window.Start+m.Rate.WindowSeconds {
			window = state.MintWindow{Start: ctx.Now}
		}
		if ctx.Now < window.Start {
			return 0, "TIME_WENT_BACKWARD"
		}
		if minted > m.Rate.Amount-window.Minted {
			return 0, "MINT_RATE_EXCEEDED"
		}
	}
	return minted, ""
}

// applyBody applies a capability's ops to the snapshot. Every value-increasing
// step is overflow-checked. Returns "" on success or a reject reason; on a
// reject the caller discards the snapshot so partial mutation never escapes.
func applyBody(body []ir.Op, snap *state.State, ctx Context, args map[string]any) string {
	for _, op := range body {
		switch op.Kind {
		case ir.OpMove:
			from, ferr := resolveAccount(op.From, ctx, args)
			if ferr != "" {
				return ferr
			}
			to, terr := resolveAccount(op.To, ctx, args)
			if terr != "" {
				return terr
			}
			amt, aerr := resolveAmount(op.Amount, args)
			if aerr != "" {
				return aerr
			}
			snap.Balances[from] -= amt
			to2, ok := addChecked(snap.Balances[to], amt)
			if !ok {
				return "OVERFLOW"
			}
			snap.Balances[to] = to2

		case ir.OpCredit:
			acct, aerr := resolveAccount(op.Account, ctx, args)
			if aerr != "" {
				return aerr
			}
			amt, aerr := resolveAmount(op.Amount, args)
			if aerr != "" {
				return aerr
			}
			bal2, ok := addChecked(snap.Balances[acct], amt)
			if !ok {
				return "OVERFLOW"
			}
			sup2, ok := addChecked(snap.Supply, amt)
			if !ok {
				return "OVERFLOW"
			}
			snap.Balances[acct] = bal2
			snap.Supply = sup2

		case ir.OpDebit:
			acct, aerr := resolveAccount(op.Account, ctx, args)
			if aerr != "" {
				return aerr
			}
			amt, aerr := resolveAmount(op.Amount, args)
			if aerr != "" {
				return aerr
			}
			snap.Balances[acct] -= amt
			snap.Supply -= amt
		}
	}
	return ""
}

// recordMintWindow advances the rate-limit window with the minted amount.
// minted was validated (and overflow-checked) in precheckMint.
func recordMintWindow(m *ir.Mint, snap *state.State, ctx Context, minted int64) {
	window, ok := snap.MintWindows[m.Name]
	if !ok || ctx.Now >= window.Start+m.Rate.WindowSeconds {
		window = state.MintWindow{Start: ctx.Now}
	}
	window.Minted += minted
	snap.MintWindows[m.Name] = window
}

// postCheckConservation verifies no balance went negative and that the ledger
// still sums to supply. Returns "" when the snapshot is sound.
func postCheckConservation(snap *state.State) string {
	for _, bal := range snap.Balances {
		if bal < 0 {
			return "INSUFFICIENT_BALANCE"
		}
	}
	var sumBal int64
	for _, v := range snap.Balances {
		sumBal += v
	}
	if sumBal != snap.Supply {
		return "CONSERVATION_VIOLATED"
	}
	return ""
}

// reject builds a failed Receipt with empty deltas and a computed hash.
func reject(base Receipt, reason string) Receipt {
	base.OK = false
	base.Reason = reason
	base.Deltas = nil
	base.SupplyDelta = 0
	base.Hash = computeHash(&base)
	return base
}

// checkAuthority enforces the authority clause on a transition or burn.
// Returns "" on success, or a reject code on failure.
// Supported form: "caller owns <id>" — caller's balance >= resolveAmount(id).
func checkAuthority(authority string, st *state.State, ctx Context, args map[string]any) string {
	// Parse "caller owns <id>"
	parts := strings.Fields(authority)
	if len(parts) == 3 && parts[0] == "caller" && parts[1] == "owns" {
		id := parts[2]
		needed, err := resolveAmount(id, args)
		if err != "" {
			return err
		}
		if st.Balances[ctx.Caller] < needed {
			return "INSUFFICIENT_BALANCE"
		}
		return ""
	}
	// Unknown authority form — permit in v1.
	return ""
}

// resolveAccount maps an identifier to a concrete account string.
func resolveAccount(id string, ctx Context, args map[string]any) (string, string) {
	if id == "caller" {
		return ctx.Caller, ""
	}
	v, ok := args[id]
	if !ok {
		return "", "BAD_ARG"
	}
	s, ok := v.(string)
	if !ok {
		return "", "BAD_ARG"
	}
	return s, ""
}

// addChecked returns a+b and reports whether the sum stayed within int64 range.
// Overflow occurs only when both operands share a sign and the result flips it.
func addChecked(a, b int64) (int64, bool) {
	s := a + b
	if (a > 0 && b > 0 && s < 0) || (a < 0 && b < 0 && s >= 0) {
		return 0, false
	}
	return s, true
}

// sumMinted totals the credited amounts in a mint body, resolving each from
// args. It returns ("" reason) on success or a reject reason ("BAD_ARG" or
// "MINT_OVERFLOW") on failure. Overflow-checking here is what keeps a
// multi-credit body from wrapping int64 past the declared cap.
func sumMinted(body []ir.Op, args map[string]any) (int64, string) {
	var minted int64
	for _, op := range body {
		if op.Kind != ir.OpCredit {
			continue
		}
		a, err := resolveAmount(op.Amount, args)
		if err != "" {
			return 0, err
		}
		sum, ok := addChecked(minted, a)
		if !ok {
			return 0, "MINT_OVERFLOW"
		}
		minted = sum
	}
	return minted, ""
}

// resolveAmount maps an identifier to an int64 amount from args.
func resolveAmount(id string, args map[string]any) (int64, string) {
	v, ok := args[id]
	if !ok {
		return 0, "BAD_ARG"
	}
	n, ok := v.(int64)
	if !ok {
		return 0, "BAD_ARG"
	}
	return n, ""
}

// computeDeltas returns the sorted, non-zero per-account balance changes
// between original state orig and snapshot snap.
func computeDeltas(orig, snap *state.State) []Delta {
	// Collect all accounts that appear in either state.
	seen := make(map[string]bool)
	for k := range orig.Balances {
		seen[k] = true
	}
	for k := range snap.Balances {
		seen[k] = true
	}

	var ds []Delta
	for acct := range seen {
		delta := snap.Balances[acct] - orig.Balances[acct]
		if delta != 0 {
			ds = append(ds, Delta{Account: acct, Amount: delta})
		}
	}
	sortDeltas(ds)
	return ds
}
