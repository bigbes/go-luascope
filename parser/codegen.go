package parser

import "math"

// The parser emits the bytecode LuaJIT emits, with a port of the code
// generator of lj_parse.c, because LuaJIT rejects code it cannot encode:
// a jump over more than 32767 instructions, an expression that needs more
// than 250 registers, more than 65536 constants. Which code that is depends
// on every instruction, register and constant, so the port follows the C
// code function by function, and the names in the comments are the names
// of the C functions. The bytecode is dropped when a function ends.

// Limits of the code generator of LuaJIT (lj_def.h).
const (
	maxSlots = 250     // LJ_MAX_SLOTS.
	maxBCIns = 1 << 26 // LJ_MAX_BCINS.
	maxConst = bcMaxD + 1
	// fr2 is LJ_FR2: Tarantool builds LuaJIT in GC64 mode, where the
	// frame of a call takes two slots.
	fr2 = 1
)

// Prototype flags (PROTO_*).
const (
	protoChild       = 0x01
	protoVararg      = 0x02
	protoHasReturn   = 0x20
	protoFixupReturn = 0x40
)

// expKind is the kind of an expression descriptor (ExpKind). The order
// matters: the constants come first.
type expKind int

const (
	vKNil expKind = iota
	vKFalse
	vKTrue
	vKStr // sval is the string.
	vKNum // nval is the number.
	// vKCdata is a 64-bit integer or an imaginary number. It is not a
	// constant expression.
	vKCdata
	vLocal     // info is the register.
	vUpval     // info is the upvalue index.
	vGlobal    // sval is the name.
	vIndexed   // info is the table register, aux the key.
	vJmp       // info is the instruction.
	vRelocable // info is the instruction.
	vNonReloc  // info is the register.
	vCall      // info is the instruction, aux the base.
	vVoid

	vKLast = vKNum
)

// expDesc is an expression descriptor (ExpDesc).
type expDesc struct {
	k    expKind
	info uint32
	aux  uint32
	nval float64
	sval string
	t, f uint32 // True and false jump lists.
}

func (e *expDesc) init(k expKind, info uint32) {
	e.k, e.info, e.t, e.f = k, info, noJump, noJump
}

func (e *expDesc) hasJump() bool      { return e.t != e.f }
func (e *expDesc) isK() bool          { return e.k <= vKLast }
func (e *expDesc) isKNoJump() bool    { return e.isK() && !e.hasJump() }
func (e *expDesc) isNumK() bool       { return e.k == vKNum }
func (e *expDesc) isNumKNoJump() bool { return e.isNumK() && !e.hasJump() }

// insLine is an emitted instruction and its line (BCInsLine).
type insLine struct {
	ins  bcIns
	line int32
}

// gcConst is a garbage-collected constant: a string, a prototype, a
// template table or cdata. Only their order is kept, for the tests.
type gcConst struct {
	kind  string // "s", "proto", "table" or "cdata".
	str   string
	proto *proto
}

// proto is a finished function, kept for the tests when parser.keep is set.
type proto struct {
	params, framesize uint32
	vararg            bool
	upvals            int
	bc                []bcIns
	knum              []float64
	kgc               []gcConst
}

// num2int converts a number to an integer like lj_num2int on x86: a
// number out of the range of int32 becomes the minimum int32.
func num2int(n float64) int32 {
	if n >= -(1<<31) && n < 1<<31 {
		return int32(n)
	}

	return math.MinInt32
}

func checki16(k int32) bool { return k == int32(int16(k)) }
func checku8(k int32) bool  { return k == int32(uint8(k)) }

// nactvar returns the number of active locals.
func (fs *funcState) nactvar() uint32 { return uint32(len(fs.actives)) }

// -- Constants ----------------------------------------------------------

// constNum adds a number constant (const_num). Numbers equal as table keys
// share a constant: 0 and -0 do.
func (fs *funcState) constNum(n float64) uint32 {
	if k, ok := fs.kn[n]; ok {
		return k
	}

	if fs.kn == nil {
		fs.kn = map[float64]uint32{}
	}

	k := fs.nkn
	fs.kn[n] = k
	fs.nkn++

	if fs.p.keep {
		fs.knum = append(fs.knum, n)
	}

	return k
}

// constGC adds a new garbage-collected constant (const_gc).
func (fs *funcState) constGC(c gcConst) uint32 {
	k := fs.nkgc
	fs.nkgc++

	if fs.p.keep {
		fs.kgc = append(fs.kgc, c)
	}

	return k
}

// constStr adds a string constant (const_str). Strings are interned, so
// equal strings share a constant.
func (fs *funcState) constStr(s string) uint32 {
	if k, ok := fs.kstr[s]; ok {
		return k
	}

	if fs.kstr == nil {
		fs.kstr = map[string]uint32{}
	}

	k := fs.constGC(gcConst{kind: "s", str: s})
	fs.kstr[s] = k

	return k
}

// -- Jump lists ---------------------------------------------------------

// jmpNext returns the next jump of a jump list (jmp_next).
func (fs *funcState) jmpNext(pc uint32) uint32 {
	delta := fs.bc[pc].ins.j()
	if uint32(delta) == noJump {
		return noJump
	}

	return uint32(int64(pc) + 1 + delta)
}

// prevIns returns the instruction before a jump (the test of a
// conditional jump), or the jump itself at pc 0.
func prevIns(pc uint32) uint32 {
	if pc >= 1 {
		return pc - 1
	}

	return pc
}

// jmpNoValue reports whether a jump of the list produces no value
// (jmp_novalue).
func (fs *funcState) jmpNoValue(list uint32) bool {
	for ; list != noJump; list = fs.jmpNext(list) {
		p := fs.bc[prevIns(list)].ins
		if p.op() != bcISTC && p.op() != bcISFC && p.a() != noReg {
			return true
		}
	}

	return false
}

// jmpPatchTestReg patches the register of a test instruction
// (jmp_patchtestreg).
func (fs *funcState) jmpPatchTestReg(pc, reg uint32) bool {
	i := prevIns(pc)
	ip := &fs.bc[i].ins

	switch op := ip.op(); {
	case op == bcISTC || op == bcISFC:
		if reg != noReg && reg != ip.d() {
			ip.setA(reg)
		} else { // Nothing to store or already in the right register.
			ip.setOp(op + (bcIST - bcISTC))
			ip.setA(0)
		}
	case ip.a() == noReg:
		if reg == noReg {
			*ip = insAJ(bcJMP, fs.bc[pc].ins.a(), 0)
		} else {
			ip.setA(reg)

			if next := &fs.bc[i+1].ins; reg >= next.a() {
				next.setA(reg + 1)
			}
		}
	default:
		return false // Cannot patch other instructions.
	}

	return true
}

// jmpDropVal drops the values of the jumps of a list (jmp_dropval).
func (fs *funcState) jmpDropVal(list uint32) {
	for ; list != noJump; list = fs.jmpNext(list) {
		fs.jmpPatchTestReg(list, noReg)
	}
}

// jmpPatchIns patches a jump to a target (jmp_patchins). A jump encodes
// its offset in 16 bits.
func (fs *funcState) jmpPatchIns(pc, dest uint32) {
	offset := dest - (pc + 1) + bcBiasJ
	if offset > bcMaxD {
		fs.p.failNear(errJump)
	}

	fs.bc[pc].ins.setD(offset)
}

// jmpAppend appends a jump list to another one (jmp_append).
func (fs *funcState) jmpAppend(l1 *uint32, l2 uint32) {
	switch {
	case l2 == noJump:
		return
	case *l1 == noJump:
		*l1 = l2
	default:
		list := *l1
		for next := fs.jmpNext(list); next != noJump; next = fs.jmpNext(list) {
			list = next
		}

		fs.jmpPatchIns(list, l2)
	}
}

// jmpPatchVal patches a jump list and keeps the values it produces
// (jmp_patchval).
func (fs *funcState) jmpPatchVal(list, vtarget, reg, dtarget uint32) {
	for list != noJump {
		next := fs.jmpNext(list)
		if fs.jmpPatchTestReg(list, reg) {
			fs.jmpPatchIns(list, vtarget) // Jump to the target with a value.
		} else {
			fs.jmpPatchIns(list, dtarget) // Jump to the default target.
		}

		list = next
	}
}

// jmpToHere makes a jump list jump to the next instruction (jmp_tohere).
func (fs *funcState) jmpToHere(list uint32) {
	fs.lasttarget = fs.pc
	fs.jmpAppend(&fs.jpc, list)
}

// jmpPatch patches a jump list to a target (jmp_patch).
func (fs *funcState) jmpPatch(list, target uint32) {
	if target == fs.pc {
		fs.jmpToHere(list)
	} else {
		fs.jmpPatchVal(list, target, noReg, target)
	}
}

// -- Registers ----------------------------------------------------------

// regBump grows the frame for n more registers (bcreg_bump).
func (fs *funcState) regBump(n uint32) {
	if sz := fs.freereg + n; sz > fs.framesize {
		if sz >= maxSlots {
			fs.p.failNear(errSlots)
		}

		fs.framesize = sz
	}
}

// regReserve reserves n registers (bcreg_reserve).
func (fs *funcState) regReserve(n uint32) {
	fs.regBump(n)
	fs.freereg += n
}

// regFree frees a register unless it holds a local (bcreg_free).
func (fs *funcState) regFree(reg uint32) {
	if reg >= fs.nactvar() {
		fs.freereg--
	}
}

// exprFree frees the register of an expression (expr_free).
func (fs *funcState) exprFree(e *expDesc) {
	if e.k == vNonReloc {
		fs.regFree(e.info)
	}
}

// -- Emitter ------------------------------------------------------------

// emit emits an instruction and patches the pending jumps to it
// (bcemit_INS). The instruction gets the line of the previous token.
func (fs *funcState) emit(ins bcIns) uint32 {
	pc := fs.pc
	fs.jmpPatchVal(fs.jpc, pc, noReg, pc)
	fs.jpc = noJump

	// The bytecode of a function and of the functions enclosing it shares
	// one stack of LJ_MAX_BCINS instructions.
	if fs.base+pc >= maxBCIns {
		fs.p.limit(fs, maxBCIns, "bytecode instructions")
	}

	if int(pc) >= len(fs.bc) {
		if int(pc) >= cap(fs.bc) {
			fs.p.growBytecode()
		}

		fs.bc = fs.bc[:pc+1]
	}

	fs.bc[pc] = insLine{ins: ins, line: int32(fs.p.lastLine)}
	fs.pc = pc + 1

	return pc
}

// growBytecode doubles the bytecode stack and moves the functions being
// parsed to the new one.
func (p *parser) growBytecode() {
	stack := make([]insLine, max(2*len(p.bcstack), 64))
	copy(stack, p.bcstack)
	p.bcstack = stack

	for fs := p.fs; fs != nil; fs = fs.prev {
		fs.bc = stack[fs.base : fs.base+uint32(len(fs.bc)) : len(stack)]
	}
}

// discharge discharges a non-constant expression to any register
// (expr_discharge).
func (fs *funcState) discharge(e *expDesc) {
	var ins bcIns

	switch e.k {
	case vUpval:
		ins = insAD(bcUGET, 0, e.info)
	case vGlobal:
		ins = insAD(bcGGET, 0, fs.constStr(e.sval))
	case vIndexed:
		switch rc := e.aux; {
		case int32(rc) < 0:
			ins = insABC(bcTGETS, 0, e.info, ^rc)
		case rc > bcMaxC:
			ins = insABC(bcTGETB, 0, e.info, rc-(bcMaxC+1))
		default:
			fs.regFree(rc)
			ins = insABC(bcTGETV, 0, e.info, rc)
		}

		fs.regFree(e.info)
	case vCall:
		e.info = e.aux
		e.k = vNonReloc

		return
	case vLocal:
		e.k = vNonReloc
		return
	default:
		return
	}

	e.info = fs.emit(ins)
	e.k = vRelocable
}

// emitNil sets a range of registers to nil (bcemit_nil).
func (fs *funcState) emitNil(from, n uint32) {
	if fs.mergeNil(from, n) {
		return
	}

	if n == 1 {
		fs.emit(insAD(bcKPRI, from, uint32(vKNil)))
	} else {
		fs.emit(insAD(bcKNIL, from, from+n-1))
	}
}

// mergeNil merges setting a range of registers to nil with the previous
// instruction if no jump targets the current position.
func (fs *funcState) mergeNil(from, n uint32) bool {
	if fs.pc <= fs.lasttarget {
		return false
	}

	ip := &fs.bc[fs.pc-1].ins
	pfrom := ip.a()

	switch ip.op() {
	case bcKPRI:
		if ip.d() != uint32(vKNil) {
			return false
		}

		switch from {
		case pfrom:
			if n == 1 {
				return true
			}
		case pfrom + 1:
			from = pfrom
			n++
		default:
			return false
		}

		*ip = insAD(bcKNIL, from, from+n-1) // Replace the KPRI.

		return true
	case bcKNIL:
		pto := ip.d()
		if from < pfrom || from > pto+1 { // Cannot connect the ranges.
			return false
		}

		if from+n-1 > pto {
			ip.setD(from + n - 1) // Extend the range of the KNIL.
		}

		return true
	default:
		return false
	}
}

// toRegNoBranch discharges an expression to a register, ignoring its
// branches (expr_toreg_nobranch).
func (fs *funcState) toRegNoBranch(e *expDesc, reg uint32) {
	var ins bcIns

	fs.discharge(e)

	switch {
	case e.k == vKStr:
		ins = insAD(bcKSTR, reg, fs.constStr(e.sval))
	case e.k == vKNum:
		if k := num2int(e.nval); checki16(k) && e.nval == float64(k) {
			ins = insAD(bcKSHORT, reg, uint32(uint16(k)))
		} else {
			ins = insAD(bcKNUM, reg, fs.constNum(e.nval))
		}
	case e.k == vKCdata:
		ins = insAD(bcKCDATA, reg, fs.constGC(gcConst{kind: "cdata"}))
	case e.k == vRelocable:
		fs.bc[e.info].ins.setA(reg)
		e.info, e.k = reg, vNonReloc

		return
	case e.k == vNonReloc:
		if reg == e.info {
			return
		}

		ins = insAD(bcMOV, reg, e.info)
	case e.k == vKNil:
		fs.emitNil(reg, 1)
		e.info, e.k = reg, vNonReloc

		return
	case e.k <= vKTrue:
		ins = insAD(bcKPRI, reg, uint32(e.k))
	default: // vVoid or vJmp.
		return
	}

	fs.emit(ins)
	e.info, e.k = reg, vNonReloc
}

// toReg discharges an expression to a register (expr_toreg).
func (fs *funcState) toReg(e *expDesc, reg uint32) {
	fs.toRegNoBranch(e, reg)

	if e.k == vJmp {
		fs.jmpAppend(&e.t, e.info) // Add it to the true jump list.
	}

	if e.hasJump() { // Discharge an expression with branches.
		jfalse, jtrue := noJump, noJump

		if fs.jmpNoValue(e.t) || fs.jmpNoValue(e.f) {
			jval := noJump
			if e.k != vJmp {
				jval = fs.jmp()
			}

			jfalse = fs.emit(insAD(bcKPRI, reg, uint32(vKFalse)))
			fs.emit(insAJ(bcJMP, fs.freereg, 1))
			jtrue = fs.emit(insAD(bcKPRI, reg, uint32(vKTrue)))
			fs.jmpToHere(jval)
		}

		jend := fs.pc
		fs.lasttarget = jend
		fs.jmpPatchVal(e.f, jend, reg, jfalse)
		fs.jmpPatchVal(e.t, jend, reg, jtrue)
	}

	e.f, e.t = noJump, noJump
	e.info, e.k = reg, vNonReloc
}

// toNextReg discharges an expression to the next free register
// (expr_tonextreg).
func (fs *funcState) toNextReg(e *expDesc) {
	fs.discharge(e)
	fs.exprFree(e)
	fs.regReserve(1)
	fs.toReg(e, fs.freereg-1)
}

// toAnyReg discharges an expression to any register (expr_toanyreg).
func (fs *funcState) toAnyReg(e *expDesc) uint32 {
	fs.discharge(e)

	if e.k == vNonReloc {
		if !e.hasJump() {
			return e.info // Already in a register.
		}

		if e.info >= fs.nactvar() {
			fs.toReg(e, e.info) // Discharge to a temporary register.
			return e.info
		}
	}

	fs.toNextReg(e)

	return e.info
}

// toVal partially discharges an expression to a value (expr_toval).
func (fs *funcState) toVal(e *expDesc) {
	if e.hasJump() {
		fs.toAnyReg(e)
	} else {
		fs.discharge(e)
	}
}

// store emits a store to an assignment target (bcemit_store).
func (fs *funcState) store(v, e *expDesc) {
	var ins bcIns

	switch v.k {
	case vLocal:
		fs.exprFree(e)
		fs.toReg(e, v.info)

		return
	case vUpval:
		fs.toVal(e)

		switch {
		case e.k <= vKTrue:
			ins = insAD(bcUSETP, v.info, uint32(e.k))
		case e.k == vKStr:
			ins = insAD(bcUSETS, v.info, fs.constStr(e.sval))
		case e.k == vKNum:
			ins = insAD(bcUSETN, v.info, fs.constNum(e.nval))
		default:
			ins = insAD(bcUSETV, v.info, fs.toAnyReg(e))
		}
	case vGlobal:
		ra := fs.toAnyReg(e)
		ins = insAD(bcGSET, ra, fs.constStr(v.sval))
	default: // vIndexed.
		ra := fs.toAnyReg(e)

		switch rc := v.aux; {
		case int32(rc) < 0:
			ins = insABC(bcTSETS, ra, v.info, ^rc)
		case rc > bcMaxC:
			ins = insABC(bcTSETB, ra, v.info, rc-(bcMaxC+1))
		default:
			ins = insABC(bcTSETV, ra, v.info, rc)
		}
	}

	fs.emit(ins)
	fs.exprFree(e)
}

// method emits the lookup of a method and the copy of the object to the
// first argument (bcemit_method).
func (fs *funcState) method(e *expDesc, key string) {
	obj := fs.toAnyReg(e)
	fs.exprFree(e)
	fn := fs.freereg
	fs.emit(insAD(bcMOV, fn+1+fr2, obj)) // Copy the object to the 1st argument.

	if idx := fs.constStr(key); idx <= bcMaxC {
		fs.regReserve(2 + fr2)
		fs.emit(insABC(bcTGETS, fn, obj, idx))
	} else {
		fs.regReserve(3 + fr2)
		fs.emit(insAD(bcKSTR, fn+2+fr2, idx))
		fs.emit(insABC(bcTGETV, fn, obj, fn+2+fr2))
		fs.freereg--
	}

	e.info, e.k = fn, vNonReloc
}

// index makes an indexed expression with a key (expr_index): a constant
// string or byte, or a register.
func (fs *funcState) index(t, e *expDesc) {
	t.k = vIndexed

	switch e.k {
	case vKNum:
		if k := num2int(e.nval); checku8(k) && e.nval == float64(k) {
			t.aux = bcMaxC + 1 + uint32(k) // 256..511: a constant byte key.
			return
		}
	case vKStr:
		if idx := fs.constStr(e.sval); idx <= bcMaxC {
			t.aux = ^idx // A constant string key.
			return
		}
	}

	t.aux = fs.toAnyReg(e) // 0..255: a register.
}

// -- Branches -----------------------------------------------------------

// jmp emits an unconditional jump (bcemit_jmp). A jump right after a UCLO
// reuses it.
func (fs *funcState) jmp() uint32 {
	jpc := fs.jpc
	j := fs.pc - 1
	fs.jpc = noJump

	if ip := &fs.bc[j].ins; int32(j) >= int32(fs.lasttarget) && ip.op() == bcUCLO {
		ip.setJ(-1) // NO_JMP.
		fs.lasttarget = j + 1
	} else {
		j = fs.emit(insAJ(bcJMP, fs.freereg, -1))
	}

	fs.jmpAppend(&j, jpc)

	return j
}

// invertCond inverts the condition of a comparison (invertcond).
func (fs *funcState) invertCond(e *expDesc) {
	ip := &fs.bc[e.info-1].ins
	ip.setOp(ip.op() ^ 1)
}

// branch emits a conditional jump (bcemit_branch).
func (fs *funcState) branch(e *expDesc, cond bool) uint32 {
	if e.k == vRelocable {
		if ip := &fs.bc[e.info].ins; ip.op() == bcNOT {
			op := bcIST
			if cond {
				op = bcISF
			}

			*ip = insAD(op, 0, ip.d())

			return fs.jmp()
		}
	}

	if e.k != vNonReloc {
		fs.regReserve(1)
		fs.toRegNoBranch(e, fs.freereg-1)
	}

	op := bcISFC
	if cond {
		op = bcISTC
	}

	fs.emit(insAD(op, noReg, e.info))
	pc := fs.jmp()
	fs.exprFree(e)

	return pc
}

// branchT emits a jump on a false condition and falls through on a true
// one (bcemit_branch_t).
func (fs *funcState) branchT(e *expDesc) {
	var pc uint32

	fs.discharge(e)

	switch e.k {
	case vKStr, vKNum, vKTrue:
		pc = noJump // Never jump.
	case vJmp:
		fs.invertCond(e)
		pc = e.info
	case vKFalse, vKNil:
		fs.toRegNoBranch(e, noReg)
		pc = fs.jmp()
	default:
		pc = fs.branch(e, false)
	}

	fs.jmpAppend(&e.f, pc)
	fs.jmpToHere(e.t)
	e.t = noJump
}

// branchF emits a jump on a true condition and falls through on a false
// one (bcemit_branch_f).
func (fs *funcState) branchF(e *expDesc) {
	var pc uint32

	fs.discharge(e)

	switch e.k {
	case vKNil, vKFalse:
		pc = noJump // Never jump.
	case vJmp:
		pc = e.info
	case vKStr, vKNum, vKTrue:
		fs.toRegNoBranch(e, noReg)
		pc = fs.jmp()
	default:
		pc = fs.branch(e, true)
	}

	fs.jmpAppend(&e.t, pc)
	fs.jmpToHere(e.f)
	e.f = noJump
}

// -- Operators ----------------------------------------------------------

// binOpr is a binary operator (BinOpr). The order matters: the opcodes
// of the arithmetic and comparison operators are computed from it.
type binOpr int

const (
	binAdd binOpr = iota
	binSub
	binMul
	binDiv
	binMod
	binPow
	binConcat
	binNe
	binEq
	binLt
	binGe
	binLe
	binGt
	binAnd
	binOr
)

// foldArith folds an arithmetic operator on number constants (foldarith).
// The power is rounded correctly, see pow.
func foldArith(opr binOpr, e1, e2 *expDesc) bool {
	if !e1.isNumKNoJump() || !e2.isNumKNoJump() {
		return false
	}

	x, y := e1.nval, e2.nval

	var n float64

	switch opr {
	case binAdd:
		n = x + y
	case binSub:
		n = x - y
	case binMul:
		n = x * y
	case binDiv:
		n = x / y
	case binMod:
		n = x - float64(math.Floor(x/y)*y) // The conversion prevents fused multiply-add.
	default:
		n = pow(x, y)
	}

	if math.IsNaN(n) || n == 0 && math.Signbit(n) {
		return false // Avoid NaN and -0 as constants.
	}

	e1.nval = n

	return true
}

// arith emits an arithmetic operator (bcemit_arith).
func (fs *funcState) arith(opr binOpr, e1, e2 *expDesc) {
	var (
		rb, rc uint32
		op     bcOp
	)

	if foldArith(opr, e1, e2) {
		return
	}

	if opr == binPow {
		op = bcPOW
		rc = fs.toAnyReg(e2)
		rb = fs.toAnyReg(e1)
	} else {
		op = bcOp(opr-binAdd) + bcADDVV
		// Discharge the 2nd operand first: an indexed one may free registers.
		fs.toVal(e2)

		if e2.isNumK() {
			rc = fs.constNum(e2.nval)
		}

		if e2.isNumK() && rc <= bcMaxC {
			op -= bcADDVV - bcADDVN
		} else {
			rc = fs.toAnyReg(e2)
		}

		// The 1st operand is discharged by binopLeft, but may need KNUM.
		fs.toVal(e1)

		var t uint32
		if e1.isNumK() && !e2.isNumK() {
			t = fs.constNum(e1.nval)
		}

		if e1.isNumK() && !e2.isNumK() && t <= bcMaxB {
			rb, rc = rc, t
			op -= bcADDVV - bcADDNV
		} else {
			rb = fs.toAnyReg(e1)
		}
	}

	fs.freeOperands(e1, e2)
	e1.info = fs.emit(insABC(op, 0, rb, rc))
	e1.k = vRelocable
}

// freeOperands frees the registers of the operands of a binary operator
// in the order of LuaJIT.
func (fs *funcState) freeOperands(e1, e2 *expDesc) {
	if e1.k == vNonReloc && e1.info >= fs.nactvar() {
		fs.freereg--
	}

	if e2.k == vNonReloc && e2.info >= fs.nactvar() {
		fs.freereg--
	}
}

// comp emits a comparison (bcemit_comp).
func (fs *funcState) comp(opr binOpr, e1, e2 *expDesc) {
	var ins bcIns

	eret := e1

	fs.toVal(e1)

	if opr == binEq || opr == binNe {
		op := bcISEQV
		if opr == binNe {
			op = bcISNEV
		}

		if e1.isK() {
			e1, e2 = e2, eret // Need the constant in the 2nd operand.
		}

		ra := fs.toAnyReg(e1) // The 1st operand must be in a register.
		fs.toVal(e2)

		switch e2.k {
		case vKNil, vKFalse, vKTrue:
			ins = insAD(op+(bcISEQP-bcISEQV), ra, uint32(e2.k))
		case vKStr:
			ins = insAD(op+(bcISEQS-bcISEQV), ra, fs.constStr(e2.sval))
		case vKNum:
			ins = insAD(op+(bcISEQN-bcISEQV), ra, fs.constNum(e2.nval))
		default:
			ins = insAD(op, ra, fs.toAnyReg(e2))
		}
	} else {
		var ra, rd uint32

		op := bcOp(opr-binLt) + bcISLT
		if (op-bcISLT)&1 != 0 { // GT -> LT, GE -> LE.
			e1, e2 = e2, eret // Swap the operands.
			op = ((op - bcISLT) ^ 3) + bcISLT
			fs.toVal(e1)
			ra = fs.toAnyReg(e1)
			rd = fs.toAnyReg(e2)
		} else {
			rd = fs.toAnyReg(e2)
			ra = fs.toAnyReg(e1)
		}

		ins = insAD(op, ra, rd)
	}

	fs.freeOperands(e1, e2)
	fs.emit(ins)
	eret.info = fs.jmp()
	eret.k = vJmp
}

// binopLeft prepares the left operand of a binary operator
// (bcemit_binop_left).
func (fs *funcState) binopLeft(opr binOpr, e *expDesc) {
	switch opr {
	case binAnd:
		fs.branchT(e)
	case binOr:
		fs.branchF(e)
	case binConcat:
		fs.toNextReg(e)
	case binEq, binNe:
		if !e.isKNoJump() {
			fs.toAnyReg(e)
		}
	default:
		if !e.isNumKNoJump() {
			fs.toAnyReg(e)
		}
	}
}

// binop emits a binary operator (bcemit_binop).
func (fs *funcState) binop(opr binOpr, e1, e2 *expDesc) {
	switch opr {
	case binAdd, binSub, binMul, binDiv, binMod, binPow:
		fs.arith(opr, e1, e2)
	case binAnd:
		fs.discharge(e2)
		fs.jmpAppend(&e2.f, e1.f)
		*e1 = *e2
	case binOr:
		fs.discharge(e2)
		fs.jmpAppend(&e2.t, e1.t)
		*e1 = *e2
	case binConcat:
		fs.toVal(e2)

		if e2.k == vRelocable && fs.bc[e2.info].ins.op() == bcCAT {
			fs.exprFree(e1)
			fs.bc[e2.info].ins.setB(e1.info)
			e1.info = e2.info
		} else {
			fs.toNextReg(e2)
			fs.exprFree(e2)
			fs.exprFree(e1)
			e1.info = fs.emit(insABC(bcCAT, 0, e1.info, e2.info))
		}

		e1.k = vRelocable
	default:
		fs.comp(opr, e1, e2)
	}
}

// unop emits a unary operator (bcemit_unop).
func (fs *funcState) unop(op bcOp, e *expDesc) {
	if op == bcNOT {
		e.f, e.t = e.t, e.f // Swap the true and false lists.
		fs.jmpDropVal(e.f)
		fs.jmpDropVal(e.t)
		fs.discharge(e)

		switch {
		case e.k == vKNil || e.k == vKFalse:
			e.k = vKTrue
			return
		case e.isK() || e.k == vKCdata:
			e.k = vKFalse
			return
		case e.k == vJmp:
			fs.invertCond(e)
			return
		case e.k == vRelocable:
			fs.regReserve(1)
			fs.bc[e.info].ins.setA(fs.freereg - 1)
			e.info, e.k = fs.freereg-1, vNonReloc
		}
	} else {
		if op == bcUNM && !e.hasJump() { // Fold negations of constants.
			if e.k == vKCdata {
				return // Folded in place: cdata is not interned.
			}

			if e.isNumK() && e.nval != 0 { // Avoid folding to -0.
				e.nval = -e.nval
				return
			}
		}

		fs.toAnyReg(e)
	}

	fs.exprFree(e)
	e.info = fs.emit(insAD(op, 0, e.info))
	e.k = vRelocable
}

// finishProto keeps a finished function for the tests.
func (fs *funcState) finishProto() *proto {
	pt := &proto{
		params:    fs.numparams,
		framesize: fs.framesize,
		vararg:    fs.flags&protoVararg != 0,
		upvals:    len(fs.uvmap),
		knum:      fs.knum,
		kgc:       fs.kgc,
		bc:        make([]bcIns, fs.pc),
	}

	for i := range fs.pc {
		pt.bc[i] = fs.bc[i].ins
	}

	// The header holds the frame size (fs_fixup_bc).
	op := bcFUNCF
	if pt.vararg {
		op = bcFUNCV
	}

	pt.bc[0] = insAD(op, fs.framesize, 0)

	return pt
}
