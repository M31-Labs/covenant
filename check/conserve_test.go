package check

import (
	"testing"

	"m31labs.dev/covenant/ir"
)

func TestTransferConserves(t *testing.T) {
	body := []ir.Op{{Kind: ir.OpMove, Ledger: "balances", From: "caller", To: "to", Amount: "amount"}}
	d := CheckConservation("transition", body)
	if d != nil {
		t.Fatalf("transfer should conserve, got %v", d)
	}
}

func TestQuietMintRejected(t *testing.T) {
	body := []ir.Op{{Kind: ir.OpCredit, Account: "caller", Amount: "amount"}}
	d := CheckConservation("transition", body)
	if d == nil || d.Code != "CONSERVE_UNMINTED" {
		t.Fatalf("want CONSERVE_UNMINTED, got %v", d)
	}
}

func TestMintMayCreateSupply(t *testing.T) {
	body := []ir.Op{{Kind: ir.OpCredit, Account: "caller", Amount: "amount"}}
	d := CheckConservation("mint", body)
	if d != nil {
		t.Fatalf("mint with OpCredit should be allowed, got %v", d)
	}
}

func TestBurnMayDestroySupply(t *testing.T) {
	body := []ir.Op{{Kind: ir.OpDebit, Account: "caller", Amount: "amount"}}
	d := CheckConservation("burn", body)
	if d != nil {
		t.Fatalf("burn with OpDebit should be allowed, got %v", d)
	}
}
