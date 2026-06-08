package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"m31labs.dev/covenant/check"
	"m31labs.dev/covenant/diag"
	"m31labs.dev/covenant/grammar"
	"m31labs.dev/covenant/interp"
	"m31labs.dev/covenant/ir"
	"m31labs.dev/covenant/state"
)

// ── entrypoint ────────────────────────────────────────────────────────────────

func main() {
	out, code := dispatch(os.Args[1:])
	fmt.Print(out)
	os.Exit(code)
}

// dispatch routes by the first argument.
func dispatch(args []string) (string, int) {
	if len(args) == 0 {
		return usage(), 1
	}
	switch args[0] {
	case "check":
		if len(args) < 2 {
			return "covenant check: missing <file>\n", 1
		}
		return runCheck(args[1])
	case "rugsurface":
		if len(args) < 2 {
			return "covenant rugsurface: missing <file>\n", 1
		}
		path, jsonOut := parseRugsurfaceArgs(args[1:])
		if path == "" {
			return "covenant rugsurface: missing <file>\n", 1
		}
		return runRugsurface(path, jsonOut)
	case "explain":
		if len(args) < 2 {
			return "covenant explain: missing <file>\n", 1
		}
		return runExplain(args[1])
	case "run":
		if len(args) < 3 {
			return "covenant run: usage: covenant run <file> <action> [key=value ...] [--caller=X] [--now=N] [--approvals=a,b]\n", 1
		}
		file, action := args[1], args[2]
		kvargs, caller, now, approvals := parseRunFlags(args[3:])
		return runRun(file, action, kvargs, caller, now, approvals)
	default:
		return fmt.Sprintf("covenant: unknown command %q\n\n", args[0]) + usage(), 1
	}
}

// ── explain ──────────────────────────────────────────────────────────────────

func runExplain(path string) (string, int) {
	_, contract, ds, exitCode := loadAndCheck(path)
	_, report := checkIfContract(contract)

	var sb strings.Builder
	name := path
	if contract != nil && contract.Name != "" {
		name = contract.Name
	}
	fmt.Fprintf(&sb, "Covenant explain — %s\n\n", name)

	if len(ds) == 0 {
		sb.WriteString("✓ static checks pass\n")
	} else {
		for _, d := range ds {
			if d.Severity == diag.Error {
				sb.WriteString("✗ static checks found a rug risk\n")
				break
			}
		}
		for _, d := range ds {
			sb.WriteString("\n")
			sb.WriteString(d.Teach())
		}
	}

	if hash, err := report.ProofHash(); err == nil && hash != "" {
		fmt.Fprintf(&sb, "proof: sha256:%s\n", hash)
	}

	if len(report.Mints) == 0 {
		sb.WriteString("\nMint power: none declared. Empty mint surface.\n")
	} else {
		sb.WriteString("\nMint power:\n")
		for _, m := range report.Mints {
			fmt.Fprintf(&sb, "- %s: cap %s %s, approval %d-of-%d", m.Name, formatAmount(m.Cap), m.Unit, m.Quorum, len(m.Signers))
			if m.Policy != "" {
				fmt.Fprintf(&sb, ", policy %s", m.Policy)
			}
			sb.WriteString("\n")
			if len(m.Signers) > 0 {
				fmt.Fprintf(&sb, "  signers: %s\n", strings.Join(m.Signers, ", "))
			}
			if m.TimelockSeconds > 0 {
				fmt.Fprintf(&sb, "  timelock: %s\n", formatSeconds(m.TimelockSeconds))
			}
			if m.Rate != nil {
				fmt.Fprintf(&sb, "  emission: at most %s %s per %s\n", formatAmount(m.Rate.Amount), m.Rate.Unit, formatSeconds(m.Rate.WindowSeconds))
			}
		}
	}

	sb.WriteString("\nAbsences:\n")
	writeAbsence(&sb, report.NoHiddenMint, "no hidden mint")
	writeAbsence(&sb, report.NoDiscretionaryPayout, "no discretionary payout")
	writeAbsence(&sb, report.NoFreeze, "no freeze")
	writeAbsence(&sb, report.NoFee, "no fee")
	writeAbsence(&sb, report.SupplyHardCapped, "supply hard-capped")

	if report.Safe() {
		sb.WriteString("\nResult: SAFE for v1 language-level guarantees. Dangerous powers are loud, bounded, and disclosed.\n")
	} else {
		sb.WriteString("\nResult: UNSAFE. Fix the diagnostics above before showing this contract to holders.\n")
	}
	return sb.String(), exitCode
}

// ── check ─────────────────────────────────────────────────────────────────────

// runCheck parses, lowers, and statically checks a Covenant file.
// Exit 0 on success (warnings ok), exit 1 if any Error-severity diagnostic.
func runCheck(path string) (string, int) {
	src, contract, ds, exitCode := loadAndCheck(path)
	_ = src

	var sb strings.Builder

	if len(ds) > 0 {
		for i, d := range ds {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(d.Teach())
		}
	}

	if exitCode != 0 {
		return sb.String(), 1
	}

	// Print any warnings, then the success line.
	if sb.Len() > 0 {
		sb.WriteString("\n")
	}

	name := "contract"
	if contract != nil {
		name = contract.Name
	}
	fmt.Fprintf(&sb, "✓ %s — checks pass. No unaccounted rug surface.\n", name)
	fmt.Fprintf(&sb, "  run `covenant rugsurface %s` to see the trust badge.\n", path)
	return sb.String(), 0
}

// ── rugsurface ────────────────────────────────────────────────────────────────

// runRugsurface prints the rug-surface trust badge for a Covenant file.
// Exits 0 even when there are errors (badge renders UNSAFE form), but exits 1
// so scripts can detect it.
func runRugsurface(path string, jsonOut bool) (string, int) {
	_, _, ds, exitCode := loadAndCheck(path)

	var sb strings.Builder
	if !jsonOut {
		for i, d := range ds {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(d.Teach())
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
	}

	// Always print the badge (even on errors — it will show unsafe form).
	_, _, report := loadForReport(path)
	if jsonOut {
		proof, err := report.JSON()
		if err != nil {
			return fmt.Sprintf("covenant rugsurface: cannot render JSON proof: %v\n", err), 1
		}
		sb.WriteString(proof)
	} else {
		sb.WriteString(report.Badge())
	}

	return sb.String(), exitCode
}

// ── run ───────────────────────────────────────────────────────────────────────

// runRun parses, checks, and executes a single action on a Covenant contract.
// Exit 0 if the action succeeds, exit 1 on any error or rejected action.
func runRun(path, action string, kvargs []string, caller string, now int64, approvals []string) (string, int) {
	src, contract, ds, exitCode := loadAndCheck(path)
	_ = src

	if exitCode != 0 {
		var sb strings.Builder
		for i, d := range ds {
			if i > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(d.Teach())
		}
		sb.WriteString("\n⚠  refusing to run an unsafe contract (fix the errors above first)\n")
		return sb.String(), 1
	}

	// Build args map.
	args := parseKVArgs(kvargs)

	ctx := interp.Context{
		Caller:    caller,
		Now:       now,
		Approvals: approvals,
	}

	st := state.New()
	receipt := interp.Invoke(contract, st, action, args, ctx)

	var sb strings.Builder
	callerStr := caller
	if callerStr == "" {
		callerStr = "anonymous"
	}
	fmt.Fprintf(&sb, "▶ %s  (caller %s, now %d)\n", action, callerStr, now)

	if receipt.OK {
		fmt.Fprintf(&sb, "  ✓ OK\n")
		for _, delta := range receipt.Deltas {
			sign := "+"
			amt := delta.Amount
			if amt < 0 {
				sign = "-"
				amt = -amt
			}
			fmt.Fprintf(&sb, "  Δ %-20s %s%s\n", delta.Account, sign, formatAmount(amt))
		}
		if receipt.SupplyDelta != 0 {
			sign := "+"
			sd := receipt.SupplyDelta
			if sd < 0 {
				sign = "-"
				sd = -sd
			}
			fmt.Fprintf(&sb, "  Δ supply               %s%s\n", sign, formatAmount(sd))
		}
		hash := receipt.Hash
		if len(hash) > 8 {
			hash = hash[:8] + "…"
		}
		fmt.Fprintf(&sb, "  receipt %s  (sha256)\n", hash)
		return sb.String(), 0
	}

	fmt.Fprintf(&sb, "  ✗ rejected: %s\n", receipt.Reason)
	return sb.String(), 1
}

// ── helpers ───────────────────────────────────────────────────────────────────

// loadAndCheck reads a file, parses, lowers, and runs Check.
// Returns the source bytes, contract (may be nil), all diagnostics, and exit code.
func loadAndCheck(path string) ([]byte, *ir.Contract, []diag.Diagnostic, int) {
	src, err := os.ReadFile(path)
	if err != nil {
		d := diag.Diagnostic{
			Severity: diag.Error,
			Line:     0,
			Code:     "CLI_READ_ERROR",
			Message:  fmt.Sprintf("cannot read %q: %v", path, err),
			Why:      "the file doesn't exist or can't be opened",
			Fix:      "check the path and permissions",
		}
		return nil, nil, []diag.Diagnostic{d}, 1
	}

	tree, parseErr := grammar.Parse(src)
	if parseErr != nil {
		d := diag.Diagnostic{
			Severity: diag.Error,
			Line:     0,
			Code:     "CLI_PARSE_ERROR",
			Message:  fmt.Sprintf("grammar error: %v", parseErr),
			Why:      "the parser itself failed (not a syntax error in the source)",
			Fix:      "check that the file is valid UTF-8 Covenant source",
		}
		return src, nil, []diag.Diagnostic{d}, 1
	}

	contract, lowerDs := ir.Lower(tree, src)
	checkDs, _ := checkIfContract(contract)

	allDs := append(lowerDs, checkDs...)

	exitCode := 0
	for _, d := range allDs {
		if d.Severity == diag.Error {
			exitCode = 1
			break
		}
	}
	return src, contract, allDs, exitCode
}

// loadForReport is like loadAndCheck but also returns the RugSurfaceReport.
// Used by rugsurface which always needs the report, even on error.
func loadForReport(path string) ([]byte, *ir.Contract, check.RugSurfaceReport) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, check.RugSurfaceReport{Contract: path}
	}
	tree, parseErr := grammar.Parse(src)
	if parseErr != nil {
		return src, nil, check.RugSurfaceReport{Contract: path}
	}
	contract, _ := ir.Lower(tree, src)
	if contract == nil {
		return src, nil, check.RugSurfaceReport{Contract: path}
	}
	_, report := check.Check(contract)
	return src, contract, report
}

// checkIfContract runs check.Check if contract is non-nil, returning empty
// slices otherwise.
func checkIfContract(contract *ir.Contract) ([]diag.Diagnostic, check.RugSurfaceReport) {
	if contract == nil {
		return nil, check.RugSurfaceReport{}
	}
	return check.Check(contract)
}

// parseKVArgs converts ["key=val", ...] into a map[string]any.
// Values that are all digits (after stripping underscores) become int64.
func parseKVArgs(kvargs []string) map[string]any {
	m := make(map[string]any, len(kvargs))
	for _, kv := range kvargs {
		idx := strings.IndexByte(kv, '=')
		if idx < 0 {
			continue
		}
		key := kv[:idx]
		val := kv[idx+1:]
		// Try to parse as int64 (digits and underscores only).
		stripped := strings.ReplaceAll(val, "_", "")
		if isAllDigits(stripped) {
			if n, err := strconv.ParseInt(stripped, 10, 64); err == nil {
				m[key] = n
				continue
			}
		}
		m[key] = val
	}
	return m
}

func parseRugsurfaceArgs(args []string) (path string, jsonOut bool) {
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		default:
			if path == "" {
				path = a
			}
		}
	}
	return path, jsonOut
}

// isAllDigits returns true if s is non-empty and every byte is '0'..'9'.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseRunFlags separates positional key=value pairs from --flag=value flags.
func parseRunFlags(args []string) (kvargs []string, caller string, now int64, approvals []string) {
	caller = "anonymous"
	now = 0
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--caller="):
			caller = strings.TrimPrefix(a, "--caller=")
		case strings.HasPrefix(a, "--now="):
			n, _ := strconv.ParseInt(strings.TrimPrefix(a, "--now="), 10, 64)
			now = n
		case strings.HasPrefix(a, "--approvals="):
			raw := strings.TrimPrefix(a, "--approvals=")
			if raw != "" {
				approvals = strings.Split(raw, ",")
			}
		case strings.HasPrefix(a, "--"):
			// ignore unknown flags
		default:
			kvargs = append(kvargs, a)
		}
	}
	return
}

// formatAmount formats an int64 with thousands separators.
func formatAmount(n int64) string {
	if n < 0 {
		return strconv.FormatInt(n, 10)
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(ch))
	}
	return string(out)
}

func formatSeconds(seconds int64) string {
	switch {
	case seconds%(24*60*60) == 0:
		n := seconds / (24 * 60 * 60)
		if n == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", n)
	case seconds%(60*60) == 0:
		n := seconds / (60 * 60)
		if n == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", n)
	case seconds%60 == 0:
		n := seconds / 60
		if n == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", n)
	default:
		if seconds == 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", seconds)
	}
}

func writeAbsence(sb *strings.Builder, ok bool, label string) {
	if ok {
		fmt.Fprintf(sb, "✓ %s\n", label)
		return
	}
	fmt.Fprintf(sb, "✗ %s\n", label)
}

// usage returns a short help string.
func usage() string {
	return `covenant — the safe cryptocurrency contract language

USAGE
  covenant check <file>                         check a contract for errors
  covenant rugsurface <file> [--json]           print the trust badge or JSON proof
  covenant explain <file>                       explain the proof in plain language
  covenant run <file> <action> [key=value ...]  execute a contract action
              [--caller=X] [--now=N] [--approvals=a,b,c]

EXAMPLES
  covenant check examples/community_token.cov
  covenant rugsurface examples/community_token.cov
  covenant explain examples/community_token.cov
  covenant run examples/community_token.cov issue recipient=alice amount=500000 \
              --caller=founder --now=1 --approvals=founder,treasurer
`
}
