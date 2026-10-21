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
// The limits on locals are both of LuaJIT's: 200 active locals in a
// function, and 65476 locals, gotos and labels declared in a function and
// the functions enclosing it.
//
// The parser also emits the bytecode LuaJIT emits for the chunk, with the
// same registers, constants and jumps, and drops it as soon as a function
// ends. This way it rejects the code that the code generator of LuaJIT
// cannot encode, with the same errors: a jump over more than 32767
// instructions ("control structure too long"), a function that needs more
// than 250 registers ("function or expression too complex"), more than
// 65536 constants in a function. Bytecode chunks are rejected.
//
// Names are resolved while parsing, using the compiler's rules, so the
// AST tells locals from globals exactly as the bytecode does.
package parser

import (
	"fmt"
	"io"
	"sync"

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
	// bcstack holds the bytecode of the functions being parsed.
	bcstack []insLine
	// fsCache holds the states of the functions by depth, see
	// newFuncState; depth is the number of functions being parsed.
	fsCache []*funcState
	depth   int
	scratch *scratch
	// lhs are the targets of the assignments being parsed, see
	// assignment.
	lhs []expDesc
	// vtop is the number of entries in the variable stack (ls->vtop).
	vtop  int
	level int
	// keep makes the parser keep the bytecode of the chunk in main, for
	// the tests.
	keep bool
	main *proto
}

// Parse reads a chunk from r and parses it. The chunk name is used in error
// messages, and the tree keeps a copy of the source in memory, see
// ParseBytes. A failure to read r is reported as a ReadError, never as a
// syntax error of the chunk.
func Parse(r io.Reader, chunk string) (*ast.Chunk, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, &ReadError{Chunk: chunk, Err: err}
	}

	return ParseBytes(src, chunk)
}

// ParseBytes parses a chunk held in memory. The chunk name is used in error
// messages and follows the convention of the chunkname of LuaJIT: the name
// of a file is passed as "@file.lua" and printed as file.lua, the start cut
// off if it is long; "=name" is printed as name; any other name is taken
// for the source of the chunk and printed as [string "name"], cut at the
// end of the first line.
//
// The names, numbers and comments of the tree are substrings of one copy
// of src. The tree does not refer to src, which the caller may reuse, but
// the copy stays in memory as long as one of these strings does, also
// after the tree is dropped: the names in globals.Usage are such strings.
// A caller keeping a few names of many chunks can copy them with
// strings.Clone.
func ParseBytes(src []byte, chunk string) (*ast.Chunk, error) {
	return newParser(src).run(chunk)
}

func newParser(src []byte) *parser {
	s, _ := scratchPool.Get().(*scratch)

	return &parser{
		lx:       newLexer(src),
		lastLine: 1,
		scratch:  s,
		bcstack:  s.bcstack,
		gola:     s.gola,
		fsCache:  s.fsCache,
		lhs:      s.lhs,
	}
}

// scratch is the memory a parser takes over from the previous chunks: the
// bytecode stack, the gotos and labels, the states of functions and the
// stack of assignment targets.
type scratch struct {
	bcstack []insLine
	gola    []golaEntry
	fsCache []*funcState
	lhs     []expDesc
}

var scratchPool = sync.Pool{New: func() any { return &scratch{} }}

// maxPooledBytecode is the size of the largest bytecode stack kept for the
// next chunks, 1 MB: the stack of a huge chunk is left to the collector.
const maxPooledBytecode = 1 << 17

// release returns the memory of the parser to the pool, without the
// references to the chunk.
func (p *parser) release() {
	for _, fs := range p.fsCache {
		clear(fs.actives[:cap(fs.actives)])
		clear(fs.pending[:cap(fs.pending)])
		clear(fs.uvmap[:cap(fs.uvmap)])
		fs.reset()
	}

	clear(p.gola[:cap(p.gola)])

	s := p.scratch
	s.gola, s.fsCache, s.lhs = p.gola[:0], p.fsCache, p.lhs[:0]
	s.bcstack = nil

	if len(p.bcstack) <= maxPooledBytecode {
		s.bcstack = p.bcstack
	}

	scratchPool.Put(s)
}

// run parses the chunk and turns a syntax error into an error.
func (p *parser) run(chunk string) (result *ast.Chunk, err error) {
	defer p.release()

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

	block := p.parse()

	return &ast.Chunk{Block: block, Comments: p.lx.comments}, nil
}

// parse parses the main function of a chunk (lj_parse).
func (p *parser) parse() *ast.Block {
	p.lx.skipHeader()

	fs := p.openFunc(0)
	fs.flags |= protoVararg       // The main chunk is a vararg function.
	fs.emit(insAD(bcFUNCV, 0, 0)) // A placeholder for the header.
	p.next()

	block := p.chunk()
	if p.tok.typ != tkEOF {
		p.errToken(tkEOF)
	}

	p.main = p.closeFunc()

	return block
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
