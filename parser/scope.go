package parser

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
)

// Limits of LuaJIT (lj_def.h).
const (
	maxLocals = 200 // LJ_MAX_LOCVAR.
	maxUpvals = 60  // LJ_MAX_UPVAL.
	maxLevels = 200 //nolint:goconst // LJ_MAX_XLEVEL, equals maxLocals by chance.
)

// breakLabel is the name of the implicit label of "break". It cannot clash
// with a user label.
const breakLabel = "(break)"

// Block scope flags (FSCOPE_*).
const (
	scopeLoop  = 1 << iota // Breakable loop.
	scopeBreak             // Has a pending break.
	scopeGola              // Has gotos or labels.
	scopeUpval             // Has a local captured by a closure.
)

// funcState is the state of a function being parsed (FuncState).
type funcState struct {
	prev *funcState
	bl   *blockScope
	// actives are the active locals by slot, like fs->varmap. Hidden loop
	// control variables are included with a nil entry.
	actives []*ast.Local
	// pending are declared locals that are not active yet.
	pending     []*ast.Local
	upvals      map[*ast.Local]bool
	vararg      bool
	lineDefined int
	golaBase    int
}

// blockScope is a lexical block (FuncScope).
type blockScope struct {
	prev    *blockScope
	nactvar int
	flags   int
	vstart  int // Start of the gotos and labels of the block in parser.gola.
}

// golaEntry is a pending goto or a label (the goto/label part of vstack).
type golaEntry struct {
	name   string
	isGoto bool
	valid  bool
	slot   int // Number of active locals at the goto or the label.
	line   int
}

func (p *parser) openFunc(line int) *funcState {
	fs := &funcState{
		prev:        p.fs,
		upvals:      map[*ast.Local]bool{},
		lineDefined: line,
		golaBase:    len(p.gola),
	}
	p.fs = fs
	p.openScope(0)

	return fs
}

func (p *parser) closeFunc() {
	p.fs.bl.flags &^= scopeUpval // Closed by the return (FSCOPE_NOCLOSE).
	p.closeScope()
	p.gola = p.gola[:p.fs.golaBase]
	p.fs = p.fs.prev
}

// limit reports a function limit error (err_limit).
func (p *parser) limit(fs *funcState, limit int, what string) {
	if fs.lineDefined == 0 {
		p.failAt(p.lx.line, fmt.Sprintf(errLimitMain, limit, what))
	}

	p.failAt(p.lx.line, fmt.Sprintf(errLimitFunc, fs.lineDefined, limit, what))
}

// declare declares a local that becomes visible with activate (var_new).
// A nil local is a hidden loop control variable.
func (p *parser) declare(v *ast.Local) {
	fs := p.fs
	if len(fs.actives)+len(fs.pending) >= maxLocals {
		p.limit(fs, maxLocals, "local variables")
	}

	fs.pending = append(fs.pending, v)
}

// activate makes the first n pending locals visible (var_add).
func (p *parser) activate(n int) {
	fs := p.fs
	fs.actives = append(fs.actives, fs.pending[:n]...)
	fs.pending = fs.pending[n:]
}

// lookup resolves a name to a local of the current or an enclosing
// function, or returns nil for a global (var_lookup).
func (p *parser) lookup(name string) *ast.Local {
	return p.lookupIn(p.fs, name)
}

func (p *parser) lookupIn(fs *funcState, name string) *ast.Local {
	if fs == nil {
		return nil
	}

	for i := len(fs.actives) - 1; i >= 0; i-- {
		if v := fs.actives[i]; v != nil && v.Name == name {
			if fs != p.fs {
				markUpval(fs, i)
			}

			return v
		}
	}

	v := p.lookupIn(fs.prev, name)
	if v != nil && !fs.upvals[v] {
		if len(fs.upvals) >= maxUpvals {
			p.limit(fs, maxUpvals, "upvalues")
		}

		fs.upvals[v] = true
	}

	return v
}

// markUpval marks the block declaring the local at the slot as having a
// captured local (fscope_uvmark).
func markUpval(fs *funcState, slot int) {
	bl := fs.bl
	for bl != nil && bl.nactvar > slot {
		bl = bl.prev
	}

	if bl != nil {
		bl.flags |= scopeUpval
	}
}

func (p *parser) openScope(flags int) {
	fs := p.fs
	fs.bl = &blockScope{
		prev:    fs.bl,
		nactvar: len(fs.actives),
		flags:   flags,
		vstart:  len(p.gola),
	}
}

// closeScope ends a block scope and resolves its gotos (fscope_end).
func (p *parser) closeScope() {
	fs := p.fs
	bl := fs.bl
	fs.bl = bl.prev
	fs.actives = fs.actives[:bl.nactvar]

	if bl.flags&scopeUpval != 0 {
		// LuaJIT closes the upvalues with a UCLO instruction, which a goto
		// right after the block reuses, see jumpLine.
		p.uclo = p.lastLine
	}

	if bl.flags&scopeBreak != 0 {
		if bl.flags&scopeLoop == 0 {
			p.golaFixup(bl)
			return
		}

		label := golaEntry{name: breakLabel, valid: true, slot: len(fs.actives)}
		p.golaResolve(bl, len(p.gola), &label)
	}

	if bl.flags&scopeGola != 0 {
		p.golaFixup(bl)
	}
}

// golaNew adds a goto or a label and returns its index.
func (p *parser) golaNew(name string, isGoto bool, line int) int {
	p.gola = append(p.gola, golaEntry{
		name:   name,
		isGoto: isGoto,
		valid:  true,
		slot:   len(p.fs.actives),
		line:   line,
	})

	return len(p.gola) - 1
}

// golaResolve resolves pending forward gotos of the block to the label at
// index idx.
func (p *parser) golaResolve(bl *blockScope, idx int, label *golaEntry) {
	for i := bl.vstart; i < idx; i++ {
		g := &p.gola[i]
		if !g.valid || !g.isGoto || g.name != label.name {
			continue
		}

		if g.slot < label.slot {
			local := p.fs.actives[g.slot]
			p.failAt(g.line, fmt.Sprintf(errGotoScope, g.name, local.Name))
		}

		g.valid = false
	}
}

// golaFixup resolves backward gotos to labels of the block going out of
// scope and moves the remaining gotos to the enclosing block.
func (p *parser) golaFixup(bl *blockScope) {
	for i := bl.vstart; i < len(p.gola); i++ {
		v := &p.gola[i]
		if !v.valid {
			continue
		}

		if !v.isGoto {
			v.valid = false

			for j := i + 1; j < len(p.gola); j++ {
				if g := &p.gola[j]; g.valid && g.isGoto && g.name == v.name {
					g.valid = false
				}
			}

			continue
		}

		if bl.prev == nil {
			if v.name == breakLabel {
				p.failAt(v.line, errBreak)
			}

			p.failAt(v.line, fmt.Sprintf(errUndefLabel, v.name))
		}

		if v.name == breakLabel {
			bl.prev.flags |= scopeBreak
		} else {
			bl.prev.flags |= scopeGola
		}

		v.slot = bl.nactvar
	}
}

// findLabel returns a label of the current block with the given name.
func (p *parser) findLabel(name string) bool {
	for i := p.fs.bl.vstart; i < len(p.gola); i++ {
		if v := &p.gola[i]; v.valid && !v.isGoto && v.name == name {
			return true
		}
	}

	return false
}

// jumpLine returns the line LuaJIT assigns to the jump of a goto or a
// break (bcemit_jmp): normally the line of the previous token, but a jump
// right after a "do" block closing upvalues reuses the UCLO instruction
// of the block and its line. Only "do" blocks leave the UCLO as the last
// instruction, other statements emit jumps or set a jump target after it.
func (p *parser) jumpLine() int {
	if p.stmtUclo > 0 {
		return p.stmtUclo
	}

	return p.lastLine
}
