package ir

import (
	"os"
	"testing"

	"m31labs.dev/covenant/grammar"
)

func TestLowerSyntaxError(t *testing.T) {
	src := []byte(`contract Bad {`)
	tree, _ := grammar.Parse(src)
	_, diags := Lower(tree, src)
	if len(diags) < 1 {
		t.Fatalf("expected at least one diagnostic, got none")
	}
	if diags[0].Code != "PARSE_SYNTAX_ERROR" {
		t.Fatalf("expected PARSE_SYNTAX_ERROR, got %q", diags[0].Code)
	}
}

func TestLowerCapOverflow(t *testing.T) {
	src := []byte(`contract Overflow {
    ledger balances: TOKEN
    supply total:    TOKEN
    invariant conserves(balances, total)

    mint issue cap 99999999999999999999999999 TOKEN by approval 1 of { admin } {
        credit recipient : amount
    }
}`)
	tree, _ := grammar.Parse(src)
	_, diags := Lower(tree, src)
	found := false
	for _, d := range diags {
		if d.Code == "LOWER_MALFORMED_MINT" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected LOWER_MALFORMED_MINT diagnostic for overflow cap, got %v", diags)
	}
}

func TestLowerMissingMintCap(t *testing.T) {
	src, err := os.ReadFile("../examples/_bad_uncapped_mint.cov")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := grammar.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c, diags := Lower(tree, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected lower diagnostics: %v", diags)
	}
	if len(c.Mints) != 1 {
		t.Fatalf("mints=%+v", c.Mints)
	}
	if c.Mints[0].Cap != -1 {
		t.Fatalf("missing cap should lower to Cap=-1, got %+v", c.Mints[0])
	}
	if c.Mints[0].Unit != "TOKEN" {
		t.Fatalf("unit=%q, want TOKEN", c.Mints[0].Unit)
	}
}

func TestLowerFlagship(t *testing.T) {
	src, _ := os.ReadFile("../examples/community_token.cov")
	tree, _ := grammar.Parse(src)
	c, diags := Lower(tree, src)
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if c.Name != "CommunityToken" {
		t.Fatalf("name=%q", c.Name)
	}
	if len(c.Ledgers) != 1 || c.Ledgers[0].Name != "balances" {
		t.Fatalf("ledgers=%+v", c.Ledgers)
	}
	if c.Supply == nil || c.Supply.Name != "total" {
		t.Fatalf("supply=%+v", c.Supply)
	}
	if len(c.Mints) != 1 {
		t.Fatalf("mints=%+v", c.Mints)
	}
	m := c.Mints[0]
	if m.Name != "issue" || m.Cap != 1_000_000 || m.Quorum != 2 || len(m.Signers) != 3 {
		t.Fatalf("mint=%+v", m)
	}
	if len(c.Transitions) != 1 {
		t.Fatalf("transitions=%+v", c.Transitions)
	}
	body := c.Transitions[0].Body
	if len(body) != 1 || body[0].Kind != OpMove {
		t.Fatalf("transfer body=%+v", body)
	}
}
