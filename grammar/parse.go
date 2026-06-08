package grammar

import "github.com/odvcencio/gotreesitter"

// Parse parses Covenant source into a syntax tree using the cached language.
func Parse(src []byte) (*gotreesitter.Tree, error) {
	p := gotreesitter.NewParser(language())
	return p.Parse(src)
}
