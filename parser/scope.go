package parser

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
)

// Limits of LuaJIT (lj_def.h and lj_parse.c).
const (
	maxLocals = 200               // LJ_MAX_LOCVAR.
	maxUpvals = 60                // LJ_MAX_UPVAL.
	maxLevels = 200               //nolint:goconst // LJ_MAX_XLEVEL, equals maxLocals by chance.
	maxVstack = 65536 - maxUpvals // LJ_MAX_VSTACK.
)

// breakLabel is the name of the implicit label of "break". It cannot clash
// with a user label.
const breakLabel = "(break)"

// Block scope flags (FSCOPE_*).
const (
	scopeLoop    = 1 << iota // Breakable loop.
	scopeBreak               // Has a pending break.
	scopeGola                // Has gotos or labels.
	scopeUpval               // Has a local captured by a closure.
	scopeNoClose             // Does not close its upvalues.
)

// funcState is the state of a function being parsed (FuncState).
type funcState struct {
	p    *parser
	prev *funcState
	bl   *blockScope
	// actives are the active locals by register, like fs->varmap. Hidden
	// loop control variables are included with a nil entry.
	actives []*ast.Local
	// pending are declared locals that are not active yet.
	pending []*ast.Local
	// uvmap are the upvalues by index.
	uvmap       []*ast.Local
	lineDefined int
	golaBase    int
	// vbase is the size of the variable stack when the function starts.
	vbase int

	// The code generator, see codegen.go. bc holds the instructions up
	// to pc. It is a part of parser.bcstack from base on: like in LuaJIT,
	// a function emits its bytecode after the bytecode of the function
	// enclosing it.
	bc         []insLine
	base       uint32
	pc         uint32
	lasttarget uint32 // The last jump target.
	jpc        uint32 // The pending jumps to the next instruction.
	freereg    uint32
	framesize  uint32
	flags      uint32
	numparams  uint32
	kn         map[float64]uint32
	kstr       map[string]uint32
	nkn, nkgc  uint32
	// knum and kgc list the constants for the tests, see proto.
	knum []float64
	kgc  []gcConst
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
	slot   int    // Number of active locals at the goto or the label.
	pc     uint32 // The jump of a goto, the target of a label.
}

// openFunc starts a function (fs_init and fscope_begin).
func (p *parser) openFunc(line int) *funcState {
	fs := p.newFuncState()
	fs.p = p
	fs.prev = p.fs
	fs.lineDefined = line
	fs.golaBase = len(p.gola)
	fs.vbase = p.vtop
	fs.jpc = noJump
	fs.framesize = 1 // The minimum frame size.

	if p.fs != nil {
		fs.base = p.fs.base + p.fs.pc
	}

	if int(fs.base) >= len(p.bcstack) {
		p.growBytecode()
	}

	fs.bc = p.bcstack[fs.base:fs.base:len(p.bcstack)]

	p.fs = fs
	p.openScope(0)

	return fs
}

// newFuncState returns a cleared function state. Functions nest, so the
// state of a function at a depth is reused by the next function at that
// depth, with the memory of its tables of constants and of its slices.
func (p *parser) newFuncState() *funcState {
	if p.depth == len(p.fsCache) {
		p.fsCache = append(p.fsCache, &funcState{})
	}

	fs := p.fsCache[p.depth]
	fs.reset()
	p.depth++

	return fs
}

// reset clears a function state and keeps the memory of its tables of
// constants and of its slices. The slices keep the locals of the previous
// function past their length.
func (fs *funcState) reset() {
	if len(fs.kn) > 0 {
		clear(fs.kn)
	}

	if len(fs.kstr) > 0 {
		clear(fs.kstr)
	}

	*fs = funcState{
		actives: fs.actives[:0],
		pending: fs.pending[:0],
		uvmap:   fs.uvmap[:0],
		kn:      fs.kn,
		kstr:    fs.kstr,
	}
}

// closeFunc finishes a function (fs_finish) and returns it if the parser
// keeps the bytecode.
func (p *parser) closeFunc() *proto {
	fs := p.fs
	p.fixupReturn()

	if fs.nkn > maxConst || fs.nkgc > maxConst {
		p.limit(fs, maxConst, "constants")
	}

	var pt *proto
	if p.keep {
		pt = fs.finishProto()
	}

	p.gola = p.gola[:fs.golaBase]
	p.vtop = fs.vbase
	p.fs = fs.prev
	p.depth--

	return pt
}

// fixupReturn adds the final return of a function and ends its scope
// (fs_fixup_ret). A function that has returned before its first closure
// was created replaces the returns with a UCLO and a jump to a copy of the
// return, so that they close the upvalues.
func (p *parser) fixupReturn() {
	fs := p.fs
	lastpc := fs.pc

	if lastpc <= fs.lasttarget || !isRetOrTail(fs.bc[lastpc-1].ins.op()) {
		if fs.bl.flags&scopeUpval != 0 {
			fs.emit(insAJ(bcUCLO, 0, 0))
		}

		fs.emit(insAD(bcRET0, 0, 1)) // Need a final return.
	}

	fs.bl.flags |= scopeNoClose // Closed above.
	p.closeScope()

	if fs.flags&protoFixupReturn == 0 {
		return
	}

	for pc := uint32(1); pc < lastpc; pc++ {
		switch ins := fs.bc[pc].ins; ins.op() {
		case bcCALLMT, bcCALLT, bcRETM, bcRET, bcRET0, bcRET1:
			offset := fs.emit(ins) // Copy the original instruction.
			fs.bc[offset].line = fs.bc[pc].line

			offset = offset - (pc + 1) + bcBiasJ
			if offset > bcMaxD {
				p.failNear(errFixup)
			}

			fs.bc[pc].ins = insAD(bcUCLO, 0, offset) // UCLO and the jump.
		case bcFNEW:
			return // Done.
		}
	}
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

	p.pushVar()
	fs.pending = append(fs.pending, v)
}

// pushVar adds an entry to the variable stack of LuaJIT (ls->vstack). Every
// local, goto and label takes an entry, which stays until the end of the
// function, so the stack limits the number of them declared in a function
// and the functions enclosing it, not the number of active ones.
func (p *parser) pushVar() {
	p.checkVar()
	p.vtop++
}

// checkVar fails if the variable stack has no room for an entry (the check
// in var_new and gola_new).
func (p *parser) checkVar() {
	if p.vtop >= maxVstack {
		p.failAt(p.lx.line, fmt.Sprintf(errVstack, maxVstack))
	}
}

// activate makes the first n pending locals visible (var_add).
func (p *parser) activate(n int) {
	fs := p.fs
	fs.actives = append(fs.actives, fs.pending[:n]...)
	fs.pending = fs.pending[n:]
}

// lookup resolves a name to a local of the current or an enclosing
// function, or returns nil for a global (var_lookup). It sets e to the
// local, the upvalue or the global.
func (p *parser) lookup(name string, e *expDesc) *ast.Local {
	return p.lookupIn(p.fs, name, e, true)
}

func (p *parser) lookupIn(fs *funcState, name string, e *expDesc, first bool) *ast.Local {
	if fs == nil { // Not found in any function: a global.
		e.init(vGlobal, 0)
		e.sval = name

		return nil
	}

	for i := len(fs.actives) - 1; i >= 0; i-- {
		if v := fs.actives[i]; v != nil && v.Name == name {
			e.init(vLocal, uint32(i))

			if !first {
				markUpval(fs, i) // The scope now has an upvalue.
			}

			return v
		}
	}

	v := p.lookupIn(fs.prev, name, e, false)
	if v != nil { // A local of an enclosing function: an upvalue here.
		e.info = uint32(p.upvalue(fs, v))
		e.k = vUpval
	}

	return v
}

// upvalue returns the index of an upvalue, adding it if needed
// (var_lookup_uv).
func (p *parser) upvalue(fs *funcState, v *ast.Local) int {
	for i, u := range fs.uvmap {
		if u == v {
			return i
		}
	}

	if len(fs.uvmap) >= maxUpvals {
		p.limit(fs, maxUpvals, "upvalues")
	}

	fs.uvmap = append(fs.uvmap, v)

	return len(fs.uvmap) - 1
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

// openScope starts a block scope (fscope_begin).
func (p *parser) openScope(flags int) {
	fs := p.fs
	fs.bl = &blockScope{
		prev:    fs.bl,
		nactvar: len(fs.actives),
		flags:   flags,
		vstart:  len(p.gola),
	}
}

// closeScope ends a block scope, closes its upvalues and resolves its
// gotos (fscope_end).
func (p *parser) closeScope() {
	fs := p.fs
	bl := fs.bl
	fs.bl = bl.prev
	fs.actives = fs.actives[:bl.nactvar]
	fs.freereg = fs.nactvar()

	if bl.flags&(scopeUpval|scopeNoClose) == scopeUpval {
		fs.emit(insAJ(bcUCLO, uint32(bl.nactvar), 0))
	}

	if bl.flags&scopeBreak != 0 {
		if bl.flags&scopeLoop == 0 { // Propagate the breaks.
			p.golaFixup(bl)
			return
		}

		// LuaJIT pushes the break label to the variable stack and drops
		// it right after resolving the breaks, so it needs room for it.
		p.checkVar()

		label := golaEntry{name: breakLabel, valid: true, slot: len(fs.actives), pc: fs.pc}
		p.golaResolve(bl, len(p.gola), &label)
	}

	if bl.flags&scopeGola != 0 {
		p.golaFixup(bl)
	}
}

// golaNew adds a goto or a label and returns its index (gola_new).
func (p *parser) golaNew(name string, isGoto bool, pc uint32) int {
	p.pushVar()
	p.gola = append(p.gola, golaEntry{
		name:   name,
		isGoto: isGoto,
		valid:  true,
		slot:   len(p.fs.actives),
		pc:     pc,
	})

	return len(p.gola) - 1
}

// golaLine returns the line of the jump of a goto, which LuaJIT reports
// errors of the goto at.
func (p *parser) golaLine(g *golaEntry) int {
	return int(p.fs.bc[g.pc].line)
}

// golaPatch makes a goto jump to a label (gola_patch).
func (p *parser) golaPatch(g, label *golaEntry) {
	fs := p.fs
	g.valid = false
	fs.bc[g.pc].ins.setA(uint32(label.slot))
	fs.jmpPatch(g.pc, label.pc)
}

// golaClose makes a goto close the upvalues of the block it leaves
// (gola_close).
func (p *parser) golaClose(g *golaEntry) {
	fs := p.fs
	fs.bc[g.pc].ins.setA(uint32(g.slot))

	if fs.bc[g.pc].ins.op() != bcJMP {
		return
	}

	if next := fs.jmpNext(g.pc); next != noJump {
		fs.jmpPatch(next, g.pc) // Jump to the UCLO.
	}

	ip := &fs.bc[g.pc].ins
	ip.setOp(bcUCLO) // Turn the jump into a UCLO.
	ip.setJ(-1)      // NO_JMP.
}

// golaResolve resolves pending forward gotos of the block to the label at
// index idx (gola_resolve).
func (p *parser) golaResolve(bl *blockScope, idx int, label *golaEntry) {
	for i := bl.vstart; i < idx; i++ {
		g := &p.gola[i]
		if !g.valid || !g.isGoto || g.name != label.name {
			continue
		}

		if g.slot < label.slot {
			local := p.fs.actives[g.slot]
			p.failAt(p.golaLine(g), fmt.Sprintf(errGotoScope, g.name, local.Name))
		}

		p.golaPatch(g, label)
	}
}

// golaFixup resolves backward gotos to labels of the block going out of
// scope and moves the remaining gotos to the enclosing block (gola_fixup).
func (p *parser) golaFixup(bl *blockScope) {
	for i := bl.vstart; i < len(p.gola); i++ {
		v := &p.gola[i]
		if !v.valid {
			continue
		}

		if !v.isGoto {
			v.valid = false // The label goes out of scope.

			for j := i + 1; j < len(p.gola); j++ { // Resolve backward gotos.
				if g := &p.gola[j]; g.valid && g.isGoto && g.name == v.name {
					if bl.flags&scopeUpval != 0 && g.slot > v.slot {
						p.golaClose(g)
					}

					p.golaPatch(g, v)
				}
			}

			continue
		}

		if bl.prev == nil { // No outer scope: an undefined label or no loop.
			if v.name == breakLabel {
				p.failAt(p.golaLine(v), errBreak)
			}

			p.failAt(p.golaLine(v), fmt.Sprintf(errUndefLabel, v.name))
		}

		if v.name == breakLabel { // Propagate the goto or break.
			bl.prev.flags |= scopeBreak
		} else {
			bl.prev.flags |= scopeGola
		}

		v.slot = bl.nactvar

		if bl.flags&scopeUpval != 0 {
			p.golaClose(v)
		}
	}
}

// findLabel returns a label of the current block with the given name.
func (p *parser) findLabel(name string) *golaEntry {
	for i := p.fs.bl.vstart; i < len(p.gola); i++ {
		if v := &p.gola[i]; v.valid && !v.isGoto && v.name == name {
			return v
		}
	}

	return nil
}
