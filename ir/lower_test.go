package ir

import (
	"os"
	"testing"

	"m31labs.dev/covenant/grammar"
)

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
