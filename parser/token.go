package parser

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
)

// tokenType is a lexical token type. Values below tkOffset are single
// characters, the same way LuaJIT encodes them.
type tokenType int

const tkOffset tokenType = 256

// Reserved words and multi-character tokens in the order of LuaJIT's TKDEF.
const (
	tkAnd tokenType = tkOffset + 1 + iota
	tkBreak
	tkDo
	tkElse
	tkElseif
	tkEnd
	tkFalse
	tkFor
	tkFunction
	tkGoto
	tkIf
	tkIn
	tkLocal
	tkNil
	tkNot
	tkOr
	tkRepeat
	tkReturn
	tkThen
	tkTrue
	tkUntil
	tkWhile
	tkConcat
	tkDots
	tkEq
	tkGe
	tkLe
	tkNe
	tkLabel
	tkNumber
	tkName
	tkString
	tkEOF
)

var tokenNames = [...]string{
	"and", "break", "do", "else", "elseif", "end", "false", "for", "function",
	"goto", "if", "in", "local", "nil", "not", "or", "repeat", "return", "then",
	"true", "until", "while",
	"..", "...", "==", ">=", "<=", "~=", "::",
	"<number>", "<name>", "<string>", "<eof>",
}

var keywords = func() map[string]tokenType {
	m := make(map[string]tokenType, tkWhile-tkAnd+1)
	for t := tkAnd; t <= tkWhile; t++ {
		m[tokenNames[t-tkAnd]] = t
	}
	return m
}()

// String returns the token representation used in error messages.
func (t tokenType) String() string {
	switch {
	case t > tkOffset:
		return tokenNames[t-tkOffset-1]
	case isCntrl(int(t)):
		return fmt.Sprintf("char(%d)", int(t))
	default:
		return string(rune(t))
	}
}

// token is a scanned token.
type token struct {
	typ tokenType
	// val is the name, the decoded string or the number lexeme.
	val string
	pos ast.Pos
}

// Character classes of LuaJIT (lj_char.c). Bytes 128-255 are identifier
// characters, so any UTF-8 text is a valid identifier.
func isDigit(c int) bool  { return c >= '0' && c <= '9' }
func isCntrl(c int) bool  { return c >= 0 && c < 32 || c == 127 }
func isXDigit(c int) bool { return isDigit(c) || c|0x20 >= 'a' && c|0x20 <= 'f' }

func isSpace(c int) bool { return c == ' ' || c >= '\t' && c <= '\r' }

func isIdent(c int) bool {
	return isDigit(c) || c|0x20 >= 'a' && c|0x20 <= 'z' || c == '_' || c >= 0x80 && c <= 0xff
}
