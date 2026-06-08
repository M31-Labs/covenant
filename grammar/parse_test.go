package grammar

import (
	"os"
	"testing"
)

func TestParseFlagshipHasNoErrors(t *testing.T) {
	src, err := os.ReadFile("../examples/community_token.cov")
	if err != nil {
		t.Fatal(err)
	}
	tree, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if tree.RootNode().HasError() {
		t.Fatalf("parse tree has ERROR nodes:\n%s", tree.RootNode().SExpr(tree.Language()))
	}
}
