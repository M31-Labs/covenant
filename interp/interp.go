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
func Invoke(c *ir.Contract, st *state.State, action string, args map[string]any, ctx Context) Receipt {
	base := Receipt{
		Contract: c.Name,
		Action:   action,
		Caller:   ctx.Caller,
		Now:      ctx.Now,
	}

	// 1. Resolve action to a mint / transition / burn.
	var (
		foundMint       *ir.Mint
		foundTransition *ir.Transition
		foundBurn       *ir.Burn
	)
	for i := range c.Mints {
		if c.Mints[i].Name == action {
			foundMint = &c.Mints[i]
			break
		}
	}
	if foundMint == nil {
		for i := range c.Transitions {
			if c.Transitions[i].Name == action {
				foundTransition = &c.Transitions[i]
				break
			}
		}
	}
	if foundMint == nil && foundTransition == nil {
		for i := range c.Burns {
			if c.Burns[i].Name == action {
				foundBurn = &c.Burns[i]
				break
			}
		}
	}
	if foundMint == nil && foundTransition == nil && foundBurn == nil {
		return reject(base, "UNKNOWN_ACTION")
	}

	// 1b. Reject any negative int64 argument — before any authority or cap check.
	// A negative amount passed to a burn does `balance -= (-N)` = balance += N,
	// which mints tokens from nothing (CRITICAL). A negative transfer drains
	// the recipient. Reject all negative amounts unconditionally.
	for _, v := range args {
		if n, ok := v.(int64); ok && n < 0 {
			return reject(base, "NEGATIVE_AMOUNT")
		}
	}

	// 2. Authority / guard checks (BEFORE snapshot mutation).
	// minted is the total this mint creates; computed once and reused for the
	// cap check, the rate pre-check, and the post-apply window update.
	var minted int64
	if foundMint != nil {
		// Quorum: count approvals that are in the signers set.
		signerSet := make(map[string]bool, len(foundMint.Signers))
		for _, s := range foundMint.Signers {
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
		if approved < foundMint.Quorum {
			return reject(base, "MINT_QUORUM_UNMET")
		}

		// Cap check: total minted by this op. sumMinted rejects on int64
		// overflow so a multi-credit body cannot wrap past the cap.
		m, reason := sumMinted(foundMint.Body, args)
		if reason != "" {
			return reject(base, reason)
		}
		minted = m
		// Overflow-safe cap check: since amounts and supply are now ≥ 0 and
		// supply ≤ cap, use subtraction to avoid int64 overflow.
		if foundMint.Cap >= 0 && minted > foundMint.Cap-st.Supply {
			return reject(base, "MINT_CAP_EXCEEDED")
		}
		if foundMint.TimelockSeconds > 0 && ctx.Now < foundMint.TimelockSeconds {
			return reject(base, "MINT_TIMELOCK_PENDING")
		}
		if foundMint.Rate != nil {
			if foundMint.Rate.WindowSeconds <= 0 || foundMint.Rate.Amount <= 0 {
				return reject(base, "MINT_RATE_INVALID")
			}
			window, ok := st.MintWindows[foundMint.Name]
			if !ok || ctx.Now >= window.Start+foundMint.Rate.WindowSeconds {
				window = state.MintWindow{Start: ctx.Now}
			}
			if ctx.Now < window.Start {
				return reject(base, "TIME_WENT_BACKWARD")
			}
			if minted > foundMint.Rate.Amount-window.Minted {
				return reject(base, "MINT_RATE_EXCEEDED")
			}
		}
	}

	if foundTransition != nil || foundBurn != nil {
		var authority string
		if foundTransition != nil {
			authority = foundTransition.Authority
		} else {
			authority = foundBurn.Authority
		}
		if reason := checkAuthority(authority, st, ctx, args); reason != "" {
			return reject(base, reason)
		}
	}

	// 3. Operate on a SNAPSHOT.
	snap := st.Clone()

	var body []ir.Op
	switch {
	case foundMint != nil:
		body = foundMint.Body
	case foundTransition != nil:
		body = foundTransition.Body
	default:
		body = foundBurn.Body
	}

	for _, op := range body {
		switch op.Kind {
		case ir.OpMove:
			from, ferr := resolveAccount(op.From, ctx, args)
			if ferr != "" {
				return reject(base, ferr)
			}
			to, terr := resolveAccount(op.To, ctx, args)
			if terr != "" {
				return reject(base, terr)
			}
			amt, aerr := resolveAmount(op.Amount, args)
			if aerr != "" {
				return reject(base, aerr)
			}
			snap.Balances[from] -= amt
			to2, ok := addChecked(snap.Balances[to], amt)
			if !ok {
				return reject(base, "OVERFLOW")
			}
			snap.Balances[to] = to2

		case ir.OpCredit:
			acct, aerr := resolveAccount(op.Account, ctx, args)
			if aerr != "" {
				return reject(base, aerr)
			}
			amt, aerr := resolveAmount(op.Amount, args)
			if aerr != "" {
				return reject(base, aerr)
			}
			bal2, ok := addChecked(snap.Balances[acct], amt)
			if !ok {
				return reject(base, "OVERFLOW")
			}
			sup2, ok := addChecked(snap.Supply, amt)
			if !ok {
				return reject(base, "OVERFLOW")
			}
			snap.Balances[acct] = bal2
			snap.Supply = sup2

		case ir.OpDebit:
			acct, aerr := resolveAccount(op.Account, ctx, args)
			if aerr != "" {
				return reject(base, aerr)
			}
			amt, aerr := resolveAmount(op.Amount, args)
			if aerr != "" {
				return reject(base, aerr)
			}
			snap.Balances[acct] -= amt
			snap.Supply -= amt
		}
	}

	if foundMint != nil && foundMint.Rate != nil {
		// minted was validated (and overflow-checked) in the pre-check above.
		window, ok := snap.MintWindows[foundMint.Name]
		if !ok || ctx.Now >= window.Start+foundMint.Rate.WindowSeconds {
			window = state.MintWindow{Start: ctx.Now}
		}
		window.Minted += minted
		snap.MintWindows[foundMint.Name] = window
	}

	// 4. Post-checks on the snapshot.
	for acct, bal := range snap.Balances {
		if bal < 0 {
			_ = acct
			return reject(base, "INSUFFICIENT_BALANCE")
		}
	}
	// Conservation: sum(Balances) must equal Supply.
	var sumBal int64
	for _, v := range snap.Balances {
		sumBal += v
	}
	if sumBal != snap.Supply {
		return reject(base, "CONSERVATION_VIOLATED")
	}

	// 5. Commit: write snapshot back into st.
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
