package grammar

import (
	"fmt"
	"sync"

	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammargen"
)

// Aliases for the grammargen DSL so the grammar definition reads cleanly.
var (
	Seq      = grammargen.Seq
	Choice   = grammargen.Choice
	Repeat   = grammargen.Repeat
	Optional = grammargen.Optional
	Field    = grammargen.Field
	Str      = grammargen.Str
	Pat      = grammargen.Pat
	Sym      = grammargen.Sym
	Token    = grammargen.Token
	CommaSep = grammargen.CommaSep
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
	g.Define("source_file", Repeat(Sym("contract")))

	// contract CommunityToken { ...decls... }
	g.Define("contract", Seq(
		Str("contract"),
		Field("name", Sym("identifier")),
		Str("{"),
		Repeat(Sym("_decl")),
		Str("}"),
	))

	// Hidden choice of the declaration forms allowed inside a contract body.
	g.Define("_decl", Choice(
		Sym("ledger_decl"),
		Sym("supply_decl"),
		Sym("invariant_decl"),
		Sym("mint_decl"),
		Sym("transition_decl"),
		Sym("burn_decl"),
	))

	// ledger balances: TOKEN
	g.Define("ledger_decl", Seq(
		Str("ledger"),
		Field("name", Sym("identifier")),
		Str(":"),
		Field("type", Sym("type")),
	))

	// supply total: TOKEN
	g.Define("supply_decl", Seq(
		Str("supply"),
		Field("name", Sym("identifier")),
		Str(":"),
		Field("type", Sym("type")),
	))

	// invariant conserves(balances, total)
	g.Define("invariant_decl", Seq(
		Str("invariant"),
		Str("conserves"),
		Str("("),
		Field("ledger", Sym("identifier")),
		Str(","),
		Field("supply", Sym("identifier")),
		Str(")"),
	))

	// mint issue cap 1_000_000 TOKEN by approval 2 of { founder, treasurer, community } { ...ops... }
	g.Define("mint_decl", Seq(
		Str("mint"),
		Field("name", Sym("identifier")),
		Str("cap"),
		Field("cap", Sym("int")),
		Field("type", Sym("type")),
		Str("by"),
		Str("approval"),
		Field("threshold", Sym("int")),
		Str("of"),
		Str("{"),
		CommaSep(Field("approver", Sym("identifier"))),
		Str("}"),
		Field("body", Sym("block")),
	))

	// transition transfer(to: Account, amount: TOKEN) by (caller owns amount) { ...ops... }
	g.Define("transition_decl", Seq(
		Str("transition"),
		Field("name", Sym("identifier")),
		Str("("),
		CommaSep(Field("param", Sym("param"))),
		Str(")"),
		Str("by"),
		Str("("),
		Field("auth", Sym("auth_expr")),
		Str(")"),
		Field("body", Sym("block")),
	))

	// param: to: Account
	g.Define("param", Seq(
		Field("name", Sym("identifier")),
		Str(":"),
		Field("type", Sym("type")),
	))

	// burn retire by (caller owns amount) { ...ops... }
	g.Define("burn_decl", Seq(
		Str("burn"),
		Field("name", Sym("identifier")),
		Str("by"),
		Str("("),
		Field("auth", Sym("auth_expr")),
		Str(")"),
		Field("body", Sym("block")),
	))

	// auth_expr: caller owns amount  (kept tiny; only what the flagship needs).
	g.Define("auth_expr", Seq(
		Field("left", Sym("identifier")),
		Field("operator", Choice(Str("owns"), Str("=="))),
		Field("right", Sym("identifier")),
	))

	// block: { ...ops... }
	g.Define("block", Seq(
		Str("{"),
		Repeat(Sym("op")),
		Str("}"),
	))

	// op: one statement inside a block.
	g.Define("op", Choice(
		Sym("move_op"),
		Sym("credit_op"),
		Sym("debit_op"),
	))

	// move balances: caller -> to : amount   (ledger : from -> to : amount)
	g.Define("move_op", Seq(
		Str("move"),
		Field("ledger", Sym("identifier")),
		Str(":"),
		Field("from", Sym("identifier")),
		Str("->"),
		Field("to", Sym("identifier")),
		Str(":"),
		Field("amount", Sym("identifier")),
	))

	// credit recipient : amount   (account : amount)
	g.Define("credit_op", Seq(
		Str("credit"),
		Field("account", Sym("identifier")),
		Str(":"),
		Field("amount", Sym("identifier")),
	))

	// debit caller : amount
	g.Define("debit_op", Seq(
		Str("debit"),
		Field("account", Sym("identifier")),
		Str(":"),
		Field("amount", Sym("identifier")),
	))

	// type: an identifier (e.g. TOKEN, Account).
	g.Define("type", Sym("identifier"))

	// identifier: [A-Za-z_][A-Za-z0-9_]*
	g.Define("identifier", Token(Pat(`[A-Za-z_][A-Za-z0-9_]*`)))

	// int: [0-9][0-9_]*  (underscores allowed, e.g. 1_000_000)
	g.Define("int", Token(Pat(`[0-9][0-9_]*`)))

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
	g.SetExtras(Pat(`[ \t\r\n]+`))

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
