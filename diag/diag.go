package diag

import "fmt"

// Severity classifies how serious a diagnostic is.
type Severity int

const (
	Error Severity = iota
	Warning
)

// Diagnostic carries a structured, teachable error or warning from any
// compiler pass. Every field is intentional: Code lets tooling filter,
// Why/Fix teach the contract author what went wrong and how to repair it.
type Diagnostic struct {
	Severity Severity
	Line, Col int
	Code     string // e.g. "LOWER_MALFORMED_MINT"
	Message  string // what's wrong
	Why      string // why it matters (teach the user)
	Fix      string // the one-line fix
}

// Teach renders a friendly, scannable teaching error.
//
//	❌ <Message>  (line <Line>)
//	   why:  <Why>
//	   fix:  <Fix>
func (d Diagnostic) Teach() string {
	icon := "❌"
	if d.Severity == Warning {
		icon = "⚠️"
	}
	var loc string
	if d.Col > 0 {
		loc = fmt.Sprintf("(line %d, col %d)", d.Line, d.Col)
	} else {
		loc = fmt.Sprintf("(line %d)", d.Line)
	}
	s := fmt.Sprintf("%s %s  %s\n", icon, d.Message, loc)
	if d.Why != "" {
		s += fmt.Sprintf("   why:  %s\n", d.Why)
	}
	if d.Fix != "" {
		s += fmt.Sprintf("   fix:  %s\n", d.Fix)
	}
	return s
}
