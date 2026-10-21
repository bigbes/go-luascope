package parser

import (
	"bytes"

	"github.com/bigbes/go-luascope/ast"
)

const eof = -1

// lexer is a port of LuaJIT's lj_lex.c that operates on an in-memory buffer.
type lexer struct {
	src       []byte
	off       int // Offset of c in src.
	c         int // Current character or eof.
	line      int // Line of c, the same as ls->linenumber.
	lineStart int // Offset of the first character of the line.
	// buf is the token buffer (ls->sb). LuaJIT reports errors near the
	// name, string and number tokens using its contents.
	buf      []byte
	comments []ast.Comment
}

func newLexer(src []byte) *lexer {
	lx := &lexer{src: src, off: -1, line: 1}
	lx.next()

	return lx
}

// next reads the next character.
func (lx *lexer) next() int {
	if lx.off < len(lx.src) {
		lx.off++
	}

	if lx.off < len(lx.src) {
		lx.c = int(lx.src[lx.off])
	} else {
		lx.c = eof
	}

	return lx.c
}

func (lx *lexer) save(c int) { lx.buf = append(lx.buf, byte(c)) }

func (lx *lexer) saveNext() int {
	lx.save(lx.c)
	return lx.next()
}

func (lx *lexer) isEOL() bool { return lx.c == '\n' || lx.c == '\r' }

func (lx *lexer) pos() ast.Pos { return ast.Pos{Line: lx.line, Col: lx.off - lx.lineStart + 1} }

// newline skips "\n", "\r", "\r\n" or "\n\r".
func (lx *lexer) newline() {
	old := lx.c
	lx.next()

	if lx.isEOL() && lx.c != old {
		lx.next()
	}

	lx.line++
	lx.lineStart = lx.off
}

// fail reports an error near the given token.
func (lx *lexer) fail(tok tokenType, msg string) {
	err := &SyntaxError{Line: lx.line, Msg: msg, HasNear: true}

	switch tok {
	case tkName, tkString, tkNumber:
		// LuaJIT passes the buffer as a C string, so a NUL ends it.
		near, _, _ := bytes.Cut(lx.buf, []byte{0})
		err.Near = string(near)
	default:
		err.Near = tok.String()
	}

	panic(err)
}

// skipHeader skips the UTF-8 BOM and the "#!" line, like lj_lex_setup.
func (lx *lexer) skipHeader() {
	header := false

	if bytes.HasPrefix(lx.src, []byte("\xef\xbb\xbf")) {
		lx.off += 2
		lx.next()

		header = true
	}

	if lx.c == '#' {
		for {
			if lx.next() == eof {
				return
			}

			if lx.isEOL() {
				break
			}
		}

		lx.newline()

		header = true
	}

	if lx.c == 0x1b { // LUA_SIGNATURE[0], a bytecode dump.
		msg := errBytecode
		if header {
			msg = errBytecodeHead
		}

		panic(&SyntaxError{Msg: msg})
	}
}

// scan returns the next token, like lex_scan.
func (lx *lexer) scan() token {
	lx.buf = lx.buf[:0]

	for {
		if isIdent(lx.c) {
			return lx.identOrNumber()
		}

		switch lx.c {
		case '\n', '\r':
			lx.newline()
		case ' ', '\t', '\v', '\f':
			lx.next()
		case '-':
			if tok, ok := lx.minusOrComment(); ok {
				return tok
			}
		case eof:
			return token{typ: tkEOF, pos: lx.pos()}
		default:
			return lx.operator()
		}
	}
}

func (lx *lexer) identOrNumber() token {
	pos := lx.pos()
	if isDigit(lx.c) {
		lx.number()
		return token{typ: tkNumber, val: string(lx.buf), pos: pos}
	}

	for isIdent(lx.saveNext()) {
	}

	name := string(lx.buf)
	if kw, ok := keywords[name]; ok {
		return token{typ: kw, val: name, pos: pos}
	}

	return token{typ: tkName, val: name, pos: pos}
}

// minusOrComment scans '-' or skips a comment, in which case ok is false.
func (lx *lexer) minusOrComment() (token, bool) {
	pos := lx.pos()
	if lx.next() != '-' {
		return token{typ: '-', pos: pos}, true
	}

	lx.next()

	start := lx.off
	if lx.c == '[' {
		if sep := lx.skipEq(); sep >= 0 {
			text := lx.longString(sep, false)
			lx.comments = append(lx.comments, ast.Comment{Pos: pos, Text: text, Long: true})
			lx.buf = lx.buf[:0]

			return token{}, false
		}

		lx.buf = lx.buf[:0]
	}

	for !lx.isEOL() && lx.c != eof {
		lx.next()
	}

	lx.comments = append(lx.comments, ast.Comment{Pos: pos, Text: string(lx.src[start:lx.off])})

	return token{}, false
}

// twoCharOps maps the first character of a two-character operator to its
// second character and token.
var twoCharOps = map[int]struct {
	second int
	typ    tokenType
}{
	'=': {'=', tkEq},
	'<': {'=', tkLe},
	'>': {'=', tkGe},
	'~': {'=', tkNe},
	':': {':', tkLabel},
}

func (lx *lexer) operator() token {
	pos := lx.pos()
	c := lx.c

	switch c {
	case '[':
		return lx.bracket(pos)
	case '"', '\'':
		return token{typ: tkString, val: lx.shortString(), pos: pos}
	case '.':
		return lx.dot(pos)
	}

	lx.next()

	if op, ok := twoCharOps[c]; ok && lx.c == op.second {
		lx.next()
		return token{typ: op.typ, pos: pos}
	}

	return token{typ: tokenType(c), pos: pos}
}

func (lx *lexer) bracket(pos ast.Pos) token {
	sep := lx.skipEq()

	switch {
	case sep >= 0:
		return token{typ: tkString, val: lx.longString(sep, true), pos: pos}
	case sep == -1:
		return token{typ: '[', pos: pos}
	default:
		lx.fail(tkString, errLongDelim)
		panic("unreachable")
	}
}

func (lx *lexer) dot(pos ast.Pos) token {
	if lx.saveNext() == '.' {
		if lx.next() == '.' {
			lx.next()
			return token{typ: tkDots, pos: pos}
		}

		return token{typ: tkConcat, pos: pos}
	}

	if !isDigit(lx.c) {
		return token{typ: '.', pos: pos}
	}

	lx.number()

	return token{typ: tkNumber, val: string(lx.buf), pos: pos}
}

// number scans a number literal, like lex_number.
func (lx *lexer) number() {
	c, xp := lx.c, int('e')

	if c == '0' && lx.saveNext()|0x20 == 'x' {
		xp = 'p'
	}

	for isIdent(lx.c) || lx.c == '.' || (lx.c == '-' || lx.c == '+') && c|0x20 == xp {
		c = lx.c
		lx.saveNext()
	}

	if !validNumber(lx.buf) {
		lx.fail(tkNumber, errNumber)
	}
}

// skipEq skips "[=*[" or "]=*]" and returns the level, like lex_skipeq.
func (lx *lexer) skipEq() int {
	count, s := 0, lx.c

	for lx.saveNext() == '=' {
		count++
	}

	if lx.c == s {
		return count
	}

	return -count - 1
}

// longString scans a long string or comment, like lex_longstring.
func (lx *lexer) longString(sep int, isString bool) string {
	lx.saveNext() // Skip the second '['.

	if lx.isEOL() { // Skip the initial newline.
		lx.newline()
	}

	for {
		switch lx.c {
		case eof:
			msg := errLongComment
			if isString {
				msg = errLongString
			}

			lx.fail(tkEOF, msg)
		case ']':
			if lx.skipEq() == sep {
				lx.saveNext() // Skip the second ']'.
				return string(lx.buf[2+sep : len(lx.buf)-2-sep])
			}
		case '\n', '\r':
			lx.save('\n')
			lx.newline()
		default:
			lx.saveNext()
		}
	}
}

// shortString scans a quoted string, like lex_string.
func (lx *lexer) shortString() string {
	delim := lx.c
	lx.saveNext()

	for lx.c != delim {
		switch lx.c {
		case eof:
			lx.fail(tkEOF, errString)
		case '\n', '\r':
			lx.fail(tkString, errString)
		case '\\':
			lx.escape()
		default:
			lx.saveNext()
		}
	}

	lx.saveNext() // Skip the trailing delimiter.

	return string(lx.buf[1 : len(lx.buf)-1])
}

var simpleEscapes = map[int]int{
	'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v',
	'\\': '\\', '"': '"', '\'': '\'',
}

// escape decodes an escape sequence starting at '\'.
func (lx *lexer) escape() {
	c := lx.next()

	if v, ok := simpleEscapes[c]; ok {
		lx.save(v)
		lx.next()

		return
	}

	switch c {
	case 'x':
		lx.save(lx.hexEscape())
		lx.next()
	case 'u':
		lx.unicodeEscape()
		lx.next()
	case 'z':
		lx.next()

		for isSpace(lx.c) {
			if lx.isEOL() {
				lx.newline()
			} else {
				lx.next()
			}
		}
	case '\n', '\r':
		lx.save('\n')
		lx.newline()
	case eof:
		// The string loop reports an unfinished string.
	default:
		lx.decimalEscape()
	}
}

// hexDigit returns the value of the current character as a hex digit.
func (lx *lexer) hexDigit() int {
	if isDigit(lx.c) {
		return lx.c & 15
	}

	if !isXDigit(lx.c) {
		lx.fail(tkString, errEscape)
	}

	return lx.c&15 + 9
}

func (lx *lexer) hexEscape() int {
	lx.next()
	c := lx.hexDigit() << 4
	lx.next()

	return c + lx.hexDigit()
}

func (lx *lexer) unicodeEscape() {
	if lx.next() != '{' {
		lx.fail(tkString, errEscape)
	}

	lx.next()

	c := 0
	for {
		c = c<<4 | lx.hexDigit()
		if c >= 0x110000 {
			lx.fail(tkString, errEscape)
		}

		if lx.next() == '}' {
			break
		}
	}

	if c >= 0xd800 && c < 0xe000 { // No surrogates.
		lx.fail(tkString, errEscape)
	}

	lx.buf = appendUTF8(lx.buf, c)
}

// appendUTF8 encodes c like LuaJIT does, without validity checks.
func appendUTF8(b []byte, c int) []byte {
	switch {
	case c < 0x80:
		return append(b, byte(c))
	case c < 0x800:
		return append(b, byte(0xc0|c>>6), byte(0x80|c&0x3f))
	case c < 0x10000:
		return append(b, byte(0xe0|c>>12), byte(0x80|c>>6&0x3f), byte(0x80|c&0x3f))
	default:
		return append(b, byte(0xf0|c>>18), byte(0x80|c>>12&0x3f),
			byte(0x80|c>>6&0x3f), byte(0x80|c&0x3f))
	}
}

func (lx *lexer) decimalEscape() {
	if !isDigit(lx.c) {
		lx.fail(tkString, errEscape)
	}

	c := lx.c - '0'
	if isDigit(lx.next()) {
		c = c*10 + lx.c - '0'
		if isDigit(lx.next()) {
			c = c*10 + lx.c - '0'
			if c > 255 {
				lx.fail(tkString, errEscape)
			}

			lx.next()
		}
	}

	lx.save(c)
}
