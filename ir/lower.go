package ir

import (
	"strconv"
	"strings"

	"github.com/odvcencio/gotreesitter"
	"m31labs.dev/covenant/diag"
)

// firstErrorNode does a pre-order walk to find the first node where
// IsError() or IsMissing() is true. Returns nil if none found.
func firstErrorNode(n *gotreesitter.Node) *gotreesitter.Node {
	if n == nil {
		return nil
	}
	if n.IsError() || n.IsMissing() {
		return n
	}
	for i := 0; i < n.ChildCount(); i++ {
		if found := firstErrorNode(n.Child(i)); found != nil {
			return found
		}
	}
	return nil
}

// Lower walks a Covenant CST and produces a *Contract.
// Any malformed declaration emits a diag.Diagnostic rather than panicking.
func Lower(tree *gotreesitter.Tree, src []byte) (*Contract, []diag.Diagnostic) {
	lang := tree.Language()
	root := tree.RootNode()
	var ds []diag.Diagnostic

	// Up-front syntax error check: if the parse tree contains any ERROR or
	// MISSING nodes, emit a single teachable diagnostic and return early.
	if root.HasError() {
		errNode := firstErrorNode(root)
		line, col := 1, 0
		if errNode != nil {
			line, col = sourceLineCol(errNode)
		}
		ds = append(ds, diag.Diagnostic{
			Severity: diag.Error,
			Line:     line,
			Col:      col,
			Code:     "PARSE_SYNTAX_ERROR",
			Message:  "syntax error — Covenant couldn't parse this file",
			Why:      "there's invalid or incomplete syntax, so the contract can't be read",
			Fix:      "check the reported line for a typo, a missing token, or an unclosed { } ( )",
		})
		return nil, ds
	}

	// source_file: Repeat(contract)
	// Find the first contract node.
	var contractNode *gotreesitter.Node
	for i := 0; i < root.NamedChildCount(); i++ {
		child := root.NamedChild(i)
		if child.Type(lang) == "contract" {
			contractNode = child
			break
		}
	}
	if contractNode == nil {
		ds = append(ds, diag.Diagnostic{
			Severity: diag.Error,
			Line:     1,
			Code:     "LOWER_NO_CONTRACT",
			Message:  "no contract declaration found",
			Why:      "every Covenant file must contain exactly one contract block",
			Fix:      "add `contract MyToken { ... }`",
		})
		return &Contract{}, ds
	}

	c := &Contract{}
	c.Name = nodeText(contractNode.ChildByFieldName("name", lang), src)

	// Walk all children to classify declarations.
	for i := 0; i < contractNode.NamedChildCount(); i++ {
		child := contractNode.NamedChild(i)
		switch child.Type(lang) {
		case "ledger_decl":
			c.Ledgers = append(c.Ledgers, lowerLedger(child, src, lang))
		case "supply_decl":
			s := lowerSupply(child, src, lang)
			c.Supply = &s
		case "invariant_decl":
			c.Invariants = append(c.Invariants, lowerInvariant(child, src, lang))
		case "mint_decl":
			m, mds := lowerMint(child, src, lang)
			ds = append(ds, mds...)
			c.Mints = append(c.Mints, m)
		case "transition_decl":
			tr := lowerTransition(child, src, lang)
			c.Transitions = append(c.Transitions, tr)
		case "burn_decl":
			b := lowerBurn(child, src, lang)
			c.Burns = append(c.Burns, b)
		}
	}

	return c, ds
}

func lowerLedger(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) Ledger {
	line, _ := sourceLineCol(n)
	return Ledger{
		Name: nodeText(n.ChildByFieldName("name", lang), src),
		Unit: nodeText(n.ChildByFieldName("type", lang), src),
		Line: line,
	}
}

func lowerSupply(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) Supply {
	line, _ := sourceLineCol(n)
	return Supply{
		Name: nodeText(n.ChildByFieldName("name", lang), src),
		Unit: nodeText(n.ChildByFieldName("type", lang), src),
		Line: line,
	}
}

func lowerInvariant(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) Invariant {
	line, _ := sourceLineCol(n)
	return Invariant{
		Kind:   "conserves",
		Ledger: nodeText(n.ChildByFieldName("ledger", lang), src),
		Supply: nodeText(n.ChildByFieldName("supply", lang), src),
		Line:   line,
	}
}

func lowerMint(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) (Mint, []diag.Diagnostic) {
	var ds []diag.Diagnostic
	line, _ := sourceLineCol(n)
	m := Mint{
		Name: nodeText(n.ChildByFieldName("name", lang), src),
		Unit: nodeText(n.ChildByFieldName("type", lang), src),
		Line: line,
		Cap:  -1,
	}

	// Parse cap literal — strip underscores before ParseInt.
	capNode := n.ChildByFieldName("cap", lang)
	if capNode != nil {
		capStr := strings.ReplaceAll(nodeText(capNode, src), "_", "")
		v, err := strconv.ParseInt(capStr, 10, 64)
		if err != nil {
			capLine, capCol := sourceLineCol(capNode)
			ds = append(ds, diag.Diagnostic{
				Severity: diag.Error,
				Line:     capLine,
				Col:      capCol,
				Code:     "LOWER_MALFORMED_MINT",
				Message:  "mint cap is too large",
				Why:      "caps must fit in a 64-bit integer (max ~9.2e18 base units)",
				Fix:      "use a smaller cap, e.g. `cap 1_000_000`",
			})
		} else {
			m.Cap = v
		}
	}

	// Parse threshold (quorum M).
	threshNode := n.ChildByFieldName("threshold", lang)
	if threshNode != nil {
		v, err := strconv.Atoi(nodeText(threshNode, src))
		if err != nil {
			threshLine, threshCol := sourceLineCol(threshNode)
			ds = append(ds, diag.Diagnostic{
				Severity: diag.Error,
				Line:     threshLine,
				Col:      threshCol,
				Code:     "LOWER_MALFORMED_MINT",
				Message:  "mint threshold is not a valid integer: " + nodeText(threshNode, src),
				Why:      "approval threshold must be a whole number",
				Fix:      "use a whole number, e.g. `approval 2 of { ... }`",
			})
		} else {
			m.Quorum = v
		}
	}

	// Collect repeated approver fields by iterating all children.
	for i := 0; i < n.ChildCount(); i++ {
		if n.FieldNameForChild(i, lang) == "approver" {
			child := n.Child(i)
			if child != nil {
				m.Signers = append(m.Signers, nodeText(child, src))
			}
		}
	}

	// Lower body block.
	bodyNode := n.ChildByFieldName("body", lang)
	if bodyNode != nil {
		m.Body = lowerBlock(bodyNode, src, lang)
	}

	return m, ds
}

func lowerTransition(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) Transition {
	line, _ := sourceLineCol(n)
	tr := Transition{
		Name: nodeText(n.ChildByFieldName("name", lang), src),
		Line: line,
	}

	// Collect repeated param fields.
	for i := 0; i < n.ChildCount(); i++ {
		if n.FieldNameForChild(i, lang) == "param" {
			child := n.Child(i)
			if child != nil {
				tr.Params = append(tr.Params, lowerParam(child, src, lang))
			}
		}
	}

	// Authority.
	authNode := n.ChildByFieldName("auth", lang)
	if authNode != nil {
		tr.Authority = lowerAuth(authNode, src, lang)
	}

	// Body.
	bodyNode := n.ChildByFieldName("body", lang)
	if bodyNode != nil {
		tr.Body = lowerBlock(bodyNode, src, lang)
	}

	return tr
}

func lowerBurn(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) Burn {
	line, _ := sourceLineCol(n)
	b := Burn{
		Name: nodeText(n.ChildByFieldName("name", lang), src),
		Line: line,
	}
	authNode := n.ChildByFieldName("auth", lang)
	if authNode != nil {
		b.Authority = lowerAuth(authNode, src, lang)
	}
	bodyNode := n.ChildByFieldName("body", lang)
	if bodyNode != nil {
		b.Body = lowerBlock(bodyNode, src, lang)
	}
	return b
}

func lowerParam(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) Param {
	return Param{
		Name: nodeText(n.ChildByFieldName("name", lang), src),
		Type: nodeText(n.ChildByFieldName("type", lang), src),
	}
}

// lowerAuth produces the joined authority string, e.g. "caller owns amount".
func lowerAuth(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) string {
	left := nodeText(n.ChildByFieldName("left", lang), src)
	op := nodeText(n.ChildByFieldName("operator", lang), src)
	right := nodeText(n.ChildByFieldName("right", lang), src)
	return strings.Join([]string{left, op, right}, " ")
}

// lowerBlock walks a block node and collects ops.
func lowerBlock(n *gotreesitter.Node, src []byte, lang *gotreesitter.Language) []Op {
	var ops []Op
	for i := 0; i < n.NamedChildCount(); i++ {
		child := n.NamedChild(i)
		line, _ := sourceLineCol(child)
		switch child.Type(lang) {
		case "move_op":
			ops = append(ops, Op{
				Kind:   OpMove,
				Ledger: nodeText(child.ChildByFieldName("ledger", lang), src),
				From:   nodeText(child.ChildByFieldName("from", lang), src),
				To:     nodeText(child.ChildByFieldName("to", lang), src),
				Amount: nodeText(child.ChildByFieldName("amount", lang), src),
				Line:   line,
			})
		case "credit_op":
			ops = append(ops, Op{
				Kind:    OpCredit,
				Account: nodeText(child.ChildByFieldName("account", lang), src),
				Amount:  nodeText(child.ChildByFieldName("amount", lang), src),
				Line:    line,
			})
		case "debit_op":
			ops = append(ops, Op{
				Kind:    OpDebit,
				Account: nodeText(child.ChildByFieldName("account", lang), src),
				Amount:  nodeText(child.ChildByFieldName("amount", lang), src),
				Line:    line,
			})
		}
	}
	return ops
}

// nodeText extracts the source text for a node, returning "" for nil.
func nodeText(n *gotreesitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	return n.Text(src)
}

// sourceLineCol returns the 1-based line and 0-based column from a node's
// start position. Both values come from the same StartPoint() call so they
// are always consistent.
func sourceLineCol(n *gotreesitter.Node) (line, col int) {
	if n == nil {
		return 0, 0
	}
	pt := n.StartPoint()
	return int(pt.Row) + 1, int(pt.Column)
}
