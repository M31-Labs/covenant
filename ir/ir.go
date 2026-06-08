package ir

type Contract struct {
	Name        string
	Policies    []Policy
	Ledgers     []Ledger
	Supply      *Supply
	Invariants  []Invariant
	Mints       []Mint
	Burns       []Burn
	Transitions []Transition
}

type Policy struct {
	Name            string
	Quorum          int
	Signers         []string
	TimelockSeconds int64
	Line            int
}
type Ledger struct {
	Name, Unit string
	Line       int
}
type Supply struct {
	Name, Unit string
	Line       int
}
type Invariant struct {
	Kind           string // Kind=="conserves"
	Ledger, Supply string
	Line           int
}
type Mint struct {
	Name            string
	Cap             int64 // hard ceiling in base units; -1 means "uncapped"
	Unit            string
	Quorum          int      // M in M-of-N
	Signers         []string // the N
	Policy          string
	TimelockSeconds int64
	Rate            *RateLimit
	Body            []Op
	Line            int
}
type RateLimit struct {
	Amount        int64
	Unit          string
	WindowSeconds int64
}
type Burn struct {
	Name, Authority string
	Body            []Op
	Line            int
}
type Transition struct {
	Name      string
	Params    []Param
	Authority string
	Body      []Op
	Line      int
}
type Param struct{ Name, Type string }

type OpKind int

const (
	OpMove OpKind = iota
	OpCredit
	OpDebit
)

type Op struct {
	Kind     OpKind
	Ledger   string // for Move
	From, To string // identifiers
	Account  string // for Credit/Debit
	Amount   string // identifier referencing a param/"amount"/"recipient"/"caller"
	Line     int
}
