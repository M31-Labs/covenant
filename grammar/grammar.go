package grammar

import (
	"fmt"
	"sync"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammargen"
)

// Aliases for the grammargen DSL so the grammar definition reads cleanly.
var (
	seq      = grammargen.Seq
	choice   = grammargen.Choice
	repeat   = grammargen.Repeat
	optional = grammargen.Optional
	field    = grammargen.Field
	str      = grammargen.Str
	pat      = grammargen.Pat
	sym      = grammargen.Sym
	token    = grammargen.Token
	commaSep = grammargen.CommaSep
)

// CovenantGrammar builds the minimal Covenant ("cov") grammar.
//
// It parses exactly the flagship token contract (examples/community_token.cov)
// with zero ERROR nodes. Productions are intentionally minimal (YAGNI): only the
// constructs that appear in the flagship file are modeled. Important children are
// @field-named so the lowering pass (Task 2) can read them structurally.
func CovenantGrammar() *grammargen.Grammar {
	g := grammargen.NewGrammar("cov")

	// source_file: one or more contracts.
	g.Define("source_file", repeat(sym("contract")))

	// contract CommunityToken { ...decls... }
	g.Define("contract", seq(
		str("contract"),
		field("name", sym("identifier")),
		str("{"),
		repeat(sym("_decl")),
		str("}"),
	))

	// Hidden choice of the declaration forms allowed inside a contract body.
	g.Define("_decl", choice(
		sym("ledger_decl"),
		sym("supply_decl"),
		sym("invariant_decl"),
		sym("mint_decl"),
		sym("transition_decl"),
		sym("burn_decl"),
	))

	// ledger balances: TOKEN
	g.Define("ledger_decl", seq(
		str("ledger"),
		field("name", sym("identifier")),
		str(":"),
		field("type", sym("type")),
	))

	// supply total: TOKEN
	g.Define("supply_decl", seq(
		str("supply"),
		field("name", sym("identifier")),
		str(":"),
		field("type", sym("type")),
	))

	// invariant conserves(balances, total)
	g.Define("invariant_decl", seq(
		str("invariant"),
		str("conserves"),
		str("("),
		field("ledger", sym("identifier")),
		str(","),
		field("supply", sym("identifier")),
		str(")"),
	))

	// mint issue cap 1_000_000 TOKEN by approval 2 of { founder, treasurer, community } { ...ops... }
	g.Define("mint_decl", seq(
		str("mint"),
		field("name", sym("identifier")),
		str("cap"),
		field("cap", sym("int")),
		field("type", sym("type")),
		str("by"),
		str("approval"),
		field("threshold", sym("int")),
		str("of"),
		str("{"),
		commaSep(field("approver", sym("identifier"))),
		str("}"),
		field("body", sym("block")),
	))

	// transition transfer(to: Account, amount: TOKEN) by (caller owns amount) { ...ops... }
	g.Define("transition_decl", seq(
		str("transition"),
		field("name", sym("identifier")),
		str("("),
		commaSep(field("param", sym("param"))),
		str(")"),
		str("by"),
		str("("),
		field("auth", sym("auth_expr")),
		str(")"),
		field("body", sym("block")),
	))

	// param: to: Account
	g.Define("param", seq(
		field("name", sym("identifier")),
		str(":"),
		field("type", sym("type")),
	))

	// burn retire by (caller owns amount) { ...ops... }
	g.Define("burn_decl", seq(
		str("burn"),
		field("name", sym("identifier")),
		str("by"),
		str("("),
		field("auth", sym("auth_expr")),
		str(")"),
		field("body", sym("block")),
	))

	// auth_expr: caller owns amount  (kept tiny; only what the flagship needs).
	g.Define("auth_expr", seq(
		field("left", sym("identifier")),
		field("operator", choice(str("owns"), str("=="))),
		field("right", sym("identifier")),
	))

	// block: { ...ops... }
	g.Define("block", seq(
		str("{"),
		repeat(sym("op")),
		str("}"),
	))

	// op: one statement inside a block.
	g.Define("op", choice(
		sym("move_op"),
		sym("credit_op"),
		sym("debit_op"),
	))

	// move balances: caller -> to : amount   (ledger : from -> to : amount)
	g.Define("move_op", seq(
		str("move"),
		field("ledger", sym("identifier")),
		str(":"),
		field("from", sym("identifier")),
		str("->"),
		field("to", sym("identifier")),
		str(":"),
		field("amount", sym("identifier")),
	))

	// credit recipient : amount   (account : amount)
	g.Define("credit_op", seq(
		str("credit"),
		field("account", sym("identifier")),
		str(":"),
		field("amount", sym("identifier")),
	))

	// debit caller : amount
	g.Define("debit_op", seq(
		str("debit"),
		field("account", sym("identifier")),
		str(":"),
		field("amount", sym("identifier")),
	))

	// type: an identifier (e.g. TOKEN, Account).
	g.Define("type", sym("identifier"))

	// identifier: [A-Za-z_][A-Za-z0-9_]*
	g.Define("identifier", token(pat(`[A-Za-z_][A-Za-z0-9_]*`)))

	// int: [0-9][0-9_]*  (underscores allowed, e.g. 1_000_000)
	g.Define("int", token(pat(`[0-9][0-9_]*`)))

	// Word token drives keyword extraction: every literal keyword (contract,
	// ledger, mint, owns, ...) is carved out of the identifier token so keywords
	// never collide with plain identifiers like total / amount / caller.
	g.SetWord("identifier")

	// Inline the hidden choice wrappers so the repeat bodies branch directly on
	// the decl/op rules. Without this, the LR generator reduces `_decl`/`op`
	// before shifting the next item, producing a large crop of (correctly
	// shift-resolved) shift/reduce conflicts in the table.
	g.SetInline("_decl", "op")

	// Whitespace is an EXTRA so it may appear between any tokens.
	g.SetExtras(pat(`[ \t\r\n]+`))

	// Embedded smoke test: parses with zero ERROR nodes.
	g.Test("flagship", flagshipSample, "")

	return g
}

const flagshipSample = `contract CommunityToken {
    ledger balances: TOKEN
    supply total:    TOKEN
    invariant conserves(balances, total)

    mint issue cap 1_000_000 TOKEN by approval 2 of { founder, treasurer, community } {
        credit recipient : amount
    }

    transition transfer(to: Account, amount: TOKEN) by (caller owns amount) {
        move balances: caller -> to : amount
    }

    burn retire by (caller owns amount) {
        debit caller : amount
    }
}
`

var (
	langOnce sync.Once
	langInst *gotreesitter.Language
	langErr  error
)

// language returns the cached generated Covenant language, building it once.
func language() *gotreesitter.Language {
	langOnce.Do(func() {
		langInst, langErr = grammargen.GenerateLanguage(CovenantGrammar())
	})
	if langErr != nil {
		panic(fmt.Sprintf("grammar: failed to generate Covenant language: %v", langErr))
	}
	return langInst
}
