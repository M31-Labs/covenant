package state

import "maps"

// State holds the runtime ledger state for a single contract.
type State struct {
	Balances map[string]int64 // holder -> amount (base units)
	Supply   int64
}

// New returns a fresh zero-balance state.
func New() *State {
	return &State{Balances: map[string]int64{}}
}

// Clone returns a deep copy of s so the interpreter can operate on a
// snapshot and only write back on success.
func (s *State) Clone() *State {
	cp := &State{
		Supply:   s.Supply,
		Balances: make(map[string]int64, len(s.Balances)),
	}
	maps.Copy(cp.Balances, s.Balances)
	return cp
}
