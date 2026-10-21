// Package parser parses Lua source code the way Tarantool's LuaJIT does.
//
// The lexer and the parser are ports of lj_lex.c and lj_parse.c of the
// LuaJIT fork used by Tarantool, built without LUAJIT_ENABLE_LUA52COMPAT.
// Besides Lua 5.1, this accepts the LuaJIT extensions: goto and labels,
// "\x", "\z" and "\u{}" escapes, binary literals, the "i", "LL" and "ULL"
// number suffixes and identifiers with bytes 128-255. "goto" is a keyword
// only before a name; a "break" must be the last statement of a block, and
// there are no empty statements.
//
// The parser does the same checks as the LuaJIT compiler: gotos, labels,
// "...", ambiguous calls and the limits on locals, upvalues and nesting.
// It does not track registers, so it accepts functions that LuaJIT rejects
// as "function or expression too complex". Bytecode chunks are rejected.
//
// Names are resolved while parsing, using the compiler's rules, so the
// AST tells locals from globals exactly as the bytecode does.
package parser

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
)

// parser is the parser state (LexState and the parser part of it).
type parser struct {
	lx       *lexer
	tok      token
	ahead    token
	hasAhead bool
	// lastLine is the line where the previous token ends (ls->lastline).
	lastLine int
	fs       *funcState
	gola     []golaEntry
	level    int
	// uclo is the line of a UCLO instruction that is the last one emitted,
	// or 0. stmtUclo is its value before the current statement.
	uclo     int
	stmtUclo int
}

// Parse parses a chunk. The chunk name is used in error messages.
func Parse(src []byte, chunk string) (result *ast.Chunk, err error) {
	p := &parser{lx: newLexer(src), lastLine: 1}

	defer func() {
		if r := recover(); r != nil {
			se, ok := r.(*SyntaxError)
			if !ok {
				panic(r)
			}

			se.Chunk = chunk
			result, err = nil, se
		}
	}()

	p.lx.skipHeader()
	p.openFunc(0)
	p.fs.vararg = true // The main chunk is a vararg function.
	p.next()

	block := p.chunk()
	if p.tok.typ != tkEOF {
		p.errToken(tkEOF)
	}

	p.closeFunc()

	return &ast.Chunk{Block: block, Comments: p.lx.comments}, nil
}

// next reads the next token (lj_lex_next).
func (p *parser) next() {
	p.lastLine = p.lx.line

	if p.hasAhead {
		p.tok, p.hasAhead = p.ahead, false
		return
	}

	p.tok = p.lx.scan()
}

// lookahead scans the token after the current one (lj_lex_lookahead).
func (p *parser) lookahead() tokenType {
	p.ahead, p.hasAhead = p.lx.scan(), true
	return p.ahead.typ
}

// failAt reports an error without a token.
func (p *parser) failAt(line int, msg string) {
	panic(&SyntaxError{Line: line, Msg: msg})
}

// failNear reports an error near the current token (err_syntax). Like
// LuaJIT, a NUL character token is not mentioned.
func (p *parser) failNear(msg string) {
	if p.tok.typ == 0 {
		p.failAt(p.lx.line, msg)
	}

	p.lx.fail(p.tok.typ, msg)
}

func (p *parser) errToken(tok tokenType) {
	p.failNear(fmt.Sprintf(errToken, tok))
}

// opt consumes an optional token.
func (p *parser) opt(tok tokenType) bool {
	if p.tok.typ == tok {
		p.next()
		return true
	}

	return false
}

// check consumes a required token.
func (p *parser) check(tok tokenType) {
	if p.tok.typ != tok {
		p.errToken(tok)
	}

	p.next()
}

// match consumes a token closing the one opened at the given line.
func (p *parser) match(what, who tokenType, line int) {
	if p.opt(what) {
		return
	}

	if line == p.lx.line {
		p.errToken(what)
	}

	p.failNear(fmt.Sprintf(errMatch, what, who, line))
}

// name consumes a name. "goto" is a name too, unless it starts a goto.
func (p *parser) name() string {
	if p.tok.typ != tkName && p.tok.typ != tkGoto {
		p.errToken(tkName)
	}

	s := p.tok.val
	p.next()

	return s
}

func (p *parser) isName() bool { return p.tok.typ == tkName || p.tok.typ == tkGoto }

// enter and leave track the syntax nesting level (synlevel_begin/end).
func (p *parser) enter() {
	p.level++
	if p.level >= maxLevels {
		p.failAt(p.lx.line, errLevels)
	}
}

func (p *parser) leave() { p.level-- }

// isBlockEnd reports whether a token ends a block (parse_isend).
func isBlockEnd(tok tokenType) bool {
	switch tok {
	case tkElse, tkElseif, tkEnd, tkUntil, tkEOF:
		return true
	default:
		return false
	}
}
