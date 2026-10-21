package parser

import (
	"fmt"
	"strings"
)

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

// idSize is LUA_IDSIZE, the size of the buffer LuaJIT formats a chunk name
// in for an error message, with the terminating NUL.
const idSize = 60

// SyntaxError is a parse error. Its text follows LuaJIT, so the same input
// and chunk name produce the same message in both.
type SyntaxError struct {
	// Chunk is the chunk name passed to Parse, as passed. Error formats it
	// the way LuaJIT does.
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

// Error returns the message in LuaJIT format: "chunk:line: msg near 'tok'",
// with the chunk name formatted like lj_debug_shortname, see ParseBytes.
// The errors of bytecode chunks have no line. They name the chunk the way
// the bytecode loader of LuaJIT does: "chunk: msg", with the "@" or "="
// dropped and a name that is bytecode itself printed as "(binary)". The
// error for bytecode after a header does not name the chunk, like LuaJIT.
func (e *SyntaxError) Error() string {
	switch {
	case e.Line != 0:
		return shortName(e.Chunk) + ":" + e.Detail()
	case e.Msg == errBytecodeHead:
		return e.Msg
	default:
		return bytecodeName(e.Chunk) + ": " + e.Msg
	}
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
	// Chunk is the chunk name passed to Parse, as passed.
	Chunk string
	// Err is the error of the reader.
	Err error
}

// Error returns "chunk: error message", with the chunk name formatted as
// in a SyntaxError.
func (e *ReadError) Error() string {
	return shortName(e.Chunk) + ": " + e.Err.Error()
}

// Unwrap returns the error of the reader, so errors.Is and errors.As reach
// it.
func (e *ReadError) Unwrap() error {
	return e.Err
}

// shortName formats a chunk name for an error message like
// lj_debug_shortname: "=name" is printed as name, cut to 59 bytes, and
// "@file" as file, with the start cut off to fit in 59 bytes. Any other name
// is taken for the source of the chunk and printed as [string "name"]; a
// name with a control character or longer than 48 bytes is cut, to at most
// 45 bytes, and ends with "...".
func shortName(chunk string) string {
	chunk = cString(chunk)

	if name, ok := strings.CutPrefix(chunk, "="); ok {
		return name[:min(len(name), idSize-1)]
	}

	if name, ok := strings.CutPrefix(chunk, "@"); ok {
		if len(name) >= idSize {
			return "..." + name[len(name)-(idSize-4):]
		}

		return name
	}

	n := 0
	for n < idSize-12 && n < len(chunk) && chunk[n] >= ' ' {
		n++
	}

	if n == len(chunk) {
		return `[string "` + chunk + `"]`
	}

	return `[string "` + chunk[:min(n, idSize-15)] + `..."]`
}

// bytecodeName formats a chunk name for an error of the bytecode loader
// like bcread_error: the "@" or "=" is dropped, and a name that starts
// with the bytecode signature, as the default name of loadstring does for
// bytecode, is printed as "(binary)".
func bytecodeName(chunk string) string {
	chunk = cString(chunk)

	switch {
	case strings.HasPrefix(chunk, "\x1b"):
		return "(binary)"
	case strings.HasPrefix(chunk, "@"), strings.HasPrefix(chunk, "="):
		return chunk[1:]
	default:
		return chunk
	}
}

// cString cuts a chunk name at the first NUL byte: LuaJIT takes the name
// as a C string.
func cString(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}

	return s
}
