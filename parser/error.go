package parser

import "fmt"

// Error messages of LuaJIT (lj_errmsg.h) used by the lexer and the parser.
const (
	errNumber       = "malformed number"
	errLongString   = "unfinished long string"
	errLongComment  = "unfinished long comment"
	errString       = "unfinished string"
	errEscape       = "invalid escape sequence"
	errLongDelim    = "invalid long string delimiter"
	errToken        = "'%s' expected"
	errMatch        = "'%s' expected (to close '%s' at line %d)"
	errLimitMain    = "main function has more than %d %s"
	errLimitFunc    = "function at line %d has more than %d %s"
	errVstack       = "chunk has more than %d local variables"
	errParam        = "<name> or '...' expected"
	errAmbiguous    = "ambiguous syntax (function call x new statement)"
	errFuncArgs     = "function arguments expected"
	errSymbol       = "unexpected symbol"
	errDots         = "cannot use '...' outside a vararg function"
	errSyntax       = "syntax error"
	errFor          = "'=' or 'in' expected"
	errBreak        = "no loop to break"
	errUndefLabel   = "undefined label '%s'"
	errDupLabel     = "duplicate label '%s'"
	errGotoScope    = "<goto %s> jumps into the scope of local '%s'"
	errLevels       = "chunk has too many syntax levels"
	errBytecode     = "cannot load bytecode"
	errBytecodeHead = "cannot load malformed bytecode"
)

// SyntaxError is a parse error. Its text follows LuaJIT, so the same input
// produces the same line, message and token in both.
type SyntaxError struct {
	// Chunk is the chunk name passed to Parse.
	Chunk string
	// Line is the line the error is reported at.
	Line int
	// Msg is the error message without the position and the token.
	Msg string
	// Near is the token the error is reported near, empty if none.
	Near string
	// HasNear is set when the message mentions a token.
	HasNear bool
}

// Error returns the message in LuaJIT format: "chunk:line: msg near 'tok'".
func (e *SyntaxError) Error() string {
	if e.Line == 0 {
		return e.Chunk + ": " + e.Msg
	}

	return e.Chunk + ":" + e.Detail()
}

// Detail returns the message without the chunk name: "line: msg near 'tok'".
// Errors without a position, like LuaJIT's bytecode errors, have Line 0
// and Detail returns just the message.
func (e *SyntaxError) Detail() string {
	if e.Line == 0 {
		return e.Msg
	}

	if e.HasNear {
		return fmt.Sprintf("%d: %s near '%s'", e.Line, e.Msg, e.Near)
	}

	return fmt.Sprintf("%d: %s", e.Line, e.Msg)
}

// ReadError is a failure to read the chunk given to Parse.
type ReadError struct {
	// Chunk is the chunk name passed to Parse.
	Chunk string
	// Err is the error of the reader.
	Err error
}

// Error returns "chunk: error message".
func (e *ReadError) Error() string {
	return e.Chunk + ": " + e.Err.Error()
}

// Unwrap returns the error of the reader, so errors.Is and errors.As reach
// it.
func (e *ReadError) Unwrap() error {
	return e.Err
}
