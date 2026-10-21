package parser

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
)

// chunk parses statements up to the end of a block (parse_chunk).
func (p *parser) chunk() *ast.Block {
	b := &ast.Block{Start: p.tok.pos}

	p.enter()

	for last := false; !last && !isBlockEnd(p.tok.typ); {
		last = p.statement(&b.Stmts)
		p.opt(';')
		p.fs.freereg = p.fs.nactvar() // Free the registers after each statement.
	}

	p.leave()

	return b
}

// block parses a chunk in a new scope (parse_block).
func (p *parser) block() *ast.Block {
	p.openScope(0)
	b := p.chunk()
	p.closeScope()

	return b
}

// statement parses a statement into stmts and reports whether it must be
// the last one in the block (parse_stmt).
func (p *parser) statement(stmts *[]ast.Stmt) bool {
	line, pos := p.lx.line, p.tok.pos

	var s ast.Stmt

	switch p.tok.typ {
	case tkIf:
		s = p.ifStmt(line)
	case tkWhile:
		s = p.whileStmt(line)
	case tkDo:
		p.next()
		s = &ast.Do{At: pos, Body: p.block()}
		p.match(tkEnd, tkDo, line)
	case tkFor:
		s = p.forStmt(line)
	case tkRepeat:
		s = p.repeatStmt(line)
	case tkFunction:
		s = p.functionStmt(line)
	case tkLocal:
		p.next()
		s = p.localStmt(pos)
	case tkLabel:
		p.label(stmts)
		return false
	default:
		return p.otherStatement(stmts)
	}

	*stmts = append(*stmts, s)

	return false
}

// otherStatement parses return, break, goto, calls and assignments.
func (p *parser) otherStatement(stmts *[]ast.Stmt) bool {
	pos := p.tok.pos

	switch {
	case p.tok.typ == tkReturn:
		*stmts = append(*stmts, p.returnStmt())
		return true
	case p.tok.typ == tkBreak:
		p.next()
		p.breakStmt()
		*stmts = append(*stmts, &ast.Break{At: pos})

		return true // Must be last in Lua 5.1.
	case p.tok.typ == tkGoto && p.lookahead() == tkName:
		p.next()
		*stmts = append(*stmts, &ast.Goto{At: pos, Label: p.gotoStmt()})
	default:
		*stmts = append(*stmts, p.callOrAssign())
	}

	return false
}

// breakStmt emits a break (parse_break).
func (p *parser) breakStmt() {
	fs := p.fs
	fs.bl.flags |= scopeBreak
	p.golaNew(breakLabel, true, fs.jmp())
}

// gotoStmt parses the label of a goto and emits the goto (parse_goto).
func (p *parser) gotoStmt() string {
	fs := p.fs
	name := p.name()

	if l := p.findLabel(name); l != nil { // A backward goto in the same scope is a loop.
		fs.emit(insAJ(bcLOOP, uint32(l.slot), -1))
	}

	fs.bl.flags |= scopeGola
	p.golaNew(name, true, fs.jmp())

	return name
}

// cond parses a condition and returns the jumps taken when it is false
// (expr_cond).
func (p *parser) cond() (ast.Expr, uint32) {
	var v expDesc

	x := p.expr(&v)
	if v.k == vKNil {
		v.k = vKFalse
	}

	p.fs.branchT(&v)

	return x, v.f
}

func (p *parser) ifStmt(line int) *ast.If {
	fs := p.fs
	s := &ast.If{At: p.tok.pos}
	escape := noJump

	flist := p.then(s)
	for p.tok.typ == tkElseif {
		fs.jmpAppend(&escape, fs.jmp())
		fs.jmpToHere(flist)
		flist = p.then(s)
	}

	if p.tok.typ == tkElse {
		fs.jmpAppend(&escape, fs.jmp())
		fs.jmpToHere(flist)
		p.next()
		s.Else = p.block()
	} else {
		fs.jmpAppend(&escape, flist)
	}

	fs.jmpToHere(escape)
	p.match(tkEnd, tkIf, line)

	return s
}

// then parses a condition and its block (parse_then).
func (p *parser) then(s *ast.If) uint32 {
	p.next() // Skip "if" or "elseif".

	c, condexit := p.cond()
	s.Conds = append(s.Conds, c)
	p.check(tkThen)
	s.Blocks = append(s.Blocks, p.block())

	return condexit
}

func (p *parser) whileStmt(line int) *ast.While {
	fs := p.fs
	s := &ast.While{At: p.tok.pos}

	p.next()

	start := fs.pc
	fs.lasttarget = start

	var condexit uint32

	s.Cond, condexit = p.cond()
	p.openScope(scopeLoop)
	p.check(tkDo)
	loop := fs.emit(insAD(bcLOOP, fs.nactvar(), 0))
	s.Body = p.block()
	fs.jmpPatch(fs.jmp(), start)
	p.match(tkEnd, tkWhile, line)
	p.closeScope()
	fs.jmpToHere(condexit)
	fs.jmpPatchIns(loop, fs.pc)

	return s
}

func (p *parser) repeatStmt(line int) *ast.Repeat {
	fs := p.fs
	s := &ast.Repeat{At: p.tok.pos}
	loop := fs.pc
	fs.lasttarget = loop

	p.openScope(scopeLoop)
	p.openScope(0)

	inner := fs.bl

	p.next()
	fs.emit(insAD(bcLOOP, fs.nactvar(), 0))
	s.Body = p.chunk()
	p.match(tkUntil, tkRepeat, line)

	var condexit uint32

	s.Cond, condexit = p.cond() // Still inside the inner scope.

	if inner.flags&scopeUpval == 0 {
		p.closeScope()
	} else {
		// Close the upvalues on both paths: a hidden break leaves the
		// loop, the jump back follows the UCLO of the inner scope.
		p.breakStmt()
		fs.jmpToHere(condexit)
		p.closeScope()
		condexit = fs.jmp()
	}

	fs.jmpPatch(condexit, loop) // Jump back if the condition is false.
	fs.jmpPatchIns(loop, fs.pc)
	p.closeScope()

	return s
}

func (p *parser) forStmt(line int) ast.Stmt {
	pos := p.tok.pos

	p.openScope(scopeLoop)
	p.next()

	v := &ast.Local{Decl: p.tok.pos}
	v.Name = p.name()

	var s ast.Stmt

	switch p.tok.typ {
	case '=':
		s = p.numericFor(pos, v, line)
	case ',', tkIn:
		s = p.genericFor(pos, v)
	default:
		p.failNear(errFor)
	}

	p.match(tkEnd, tkFor, line)
	p.closeScope() // Resolve the breaks.

	return s
}

// declareHidden declares the three hidden loop control variables.
func (p *parser) declareHidden() {
	for range 3 {
		p.declare(nil)
	}
}

// numericFor parses a numeric for loop (parse_for_num). The loop control
// instructions are at the end of the loop.
func (p *parser) numericFor(pos ast.Pos, v *ast.Local, line int) *ast.NumericFor {
	fs := p.fs
	s := &ast.NumericFor{At: pos, Var: v}
	base := fs.freereg

	p.declareHidden()
	p.declare(v)
	p.check('=')
	s.Start = p.exprNext()
	p.check(',')
	s.Limit = p.exprNext()

	if p.opt(',') {
		s.Step = p.exprNext()
	} else {
		fs.emit(insAD(bcKSHORT, fs.freereg, 1)) // The default step is 1.
		fs.regReserve(1)
	}

	p.activate(3)
	p.check(tkDo)
	loop := fs.emit(insAJ(bcFORI, base, -1))
	p.openScope(0)
	p.activate(1)
	fs.regReserve(1)
	s.Body = p.block()
	p.closeScope()

	loopend := fs.emit(insAJ(bcFORL, base, -1))
	fs.bc[loopend].line = int32(line)
	fs.jmpPatchIns(loopend, loop+1)
	fs.jmpPatchIns(loop, fs.pc)

	return s
}

// genericFor parses a generic for loop (parse_for_iter).
func (p *parser) genericFor(pos ast.Pos, first *ast.Local) *ast.GenericFor {
	fs := p.fs
	s := &ast.GenericFor{At: pos, Vars: []*ast.Local{first}}
	base := fs.freereg + 3
	exprpc := fs.pc

	p.declareHidden()
	p.declare(first)

	for p.opt(',') {
		v := &ast.Local{Decl: p.tok.pos}
		v.Name = p.name()
		p.declare(v)
		s.Vars = append(s.Vars, v)
	}

	p.check(tkIn)

	var e expDesc

	line := p.lx.line
	s.Exprs = p.exprList(&e)
	p.assignAdjust(3, len(s.Exprs), &e)
	fs.regBump(3 + fr2) // The iterator needs another 4 slots.

	isnext := len(s.Vars) <= 2 && fs.pc > exprpc && p.predictNext(exprpc)

	p.activate(3)
	p.check(tkDo)

	op, iter := bcJMP, bcITERC
	if isnext {
		op, iter = bcISNEXT, bcITERN
	}

	loop := fs.emit(insAJ(op, base, -1))
	p.openScope(0)
	p.activate(len(s.Vars))
	fs.regReserve(uint32(len(s.Vars)))
	s.Body = p.block()
	p.closeScope()

	fs.jmpPatchIns(loop, fs.pc)
	fs.emit(insABC(iter, base, uint32(len(s.Vars))+1, 2+1))
	loopend := fs.emit(insAJ(bcITERL, base, -1))
	fs.bc[loopend-1].line = int32(line)
	fs.bc[loopend].line = int32(line)
	fs.jmpPatchIns(loopend, loop+1)

	return s
}

// predictNext guesses from the first instruction of the iterator whether
// it is next() or pairs() (predict_next).
func (p *parser) predictNext(pc uint32) bool {
	fs := p.fs
	ins := fs.bc[pc].ins

	var name string

	switch ins.op() {
	case bcMOV:
		if ins.d() >= fs.nactvar() || fs.actives[ins.d()] == nil {
			return false
		}

		name = fs.actives[ins.d()].Name
	case bcUGET:
		name = fs.uvmap[ins.d()].Name
	case bcGGET:
		for _, s := range []string{"pairs", "next"} {
			if k, ok := fs.kstr[s]; ok && k == ins.d() {
				return true
			}
		}

		return false
	default:
		return false
	}

	return name == "pairs" || name == "next"
}

func (p *parser) functionStmt(line int) *ast.FunctionStmt {
	var v, b expDesc

	fs := p.fs
	s := &ast.FunctionStmt{At: p.tok.pos}

	p.next()

	pos := p.tok.pos
	name := p.name()

	var target ast.Expr = &ast.Name{At: pos, Name: name, Local: p.lookup(name, &v)}

	for p.tok.typ == '.' {
		target = p.field(target, &v)
	}

	if p.tok.typ == ':' {
		s.Method = true
		target = p.field(target, &v)
	}

	s.Target = target
	s.Func = p.body(&b, s.At, s.Method, line)
	fs.store(&v, &b)
	fs.bc[fs.pc-1].line = int32(line) // The line of the store.

	return s
}

func (p *parser) localStmt(pos ast.Pos) ast.Stmt {
	fs := p.fs

	if p.opt(tkFunction) {
		var lv, b expDesc

		v := &ast.Local{Decl: p.tok.pos}
		v.Name = p.name()
		p.declare(v)
		lv.init(vLocal, fs.freereg)
		fs.regReserve(1)
		p.activate(1)

		f := p.body(&b, pos, false, p.lx.line)
		fs.exprFree(&b)
		fs.toReg(&b, lv.info)

		return &ast.LocalFunction{At: pos, Name: v, Func: f}
	}

	s := &ast.LocalStmt{At: pos}

	for {
		v := &ast.Local{Decl: p.tok.pos}
		v.Name = p.name()
		p.declare(v)
		s.Names = append(s.Names, v)

		if !p.opt(',') {
			break
		}
	}

	var e expDesc

	if p.opt('=') {
		s.Values = p.exprList(&e)
	} else {
		e.k = vVoid
	}

	p.assignAdjust(len(s.Names), len(s.Values), &e)
	p.activate(len(s.Names))

	return s
}

// assignAdjust adjusts the values of an assignment to the targets
// (assign_adjust): a call returns as many results as needed, missing
// values are nil, extra values are dropped.
func (p *parser) assignAdjust(nvars, nexps int, e *expDesc) {
	fs := p.fs
	extra := nvars - nexps

	if e.k == vCall {
		extra++ // Compensate for the call itself.
		if extra < 0 {
			extra = 0
		}

		fs.bc[e.info].ins.setB(uint32(extra + 1)) // Fix the call results.

		if extra > 1 {
			fs.regReserve(uint32(extra - 1))
		}
	} else {
		if e.k != vVoid {
			fs.toNextReg(e) // Close the last expression.
		}

		if extra > 0 { // Set the leftover targets to nil.
			reg := fs.freereg
			fs.regReserve(uint32(extra))
			fs.emitNil(reg, uint32(extra))
		}
	}

	if nexps > nvars {
		fs.freereg -= uint32(nexps - nvars) // Drop the leftover registers.
	}
}

func (p *parser) returnStmt() *ast.Return {
	var ins bcIns

	fs := p.fs
	s := &ast.Return{At: p.tok.pos}

	p.next()

	fs.flags |= protoHasReturn

	if isBlockEnd(p.tok.typ) || p.tok.typ == ';' {
		ins = insAD(bcRET0, 0, 1)
	} else {
		ins = p.returnValues(s)
	}

	if fs.flags&protoChild != 0 {
		fs.emit(insAJ(bcUCLO, 0, 0)) // May need to close upvalues first.
	}

	fs.emit(ins)

	return s
}

// returnValues parses the values of a return and returns the instruction
// that returns them.
func (p *parser) returnValues(s *ast.Return) bcIns {
	var e expDesc

	fs := p.fs
	s.Values = p.exprList(&e)

	if e.k == vCall {
		call := fs.bc[e.info].ins
		// A tail call; it does not pay off to add VARGT for "return ...".
		if len(s.Values) == 1 && call.op() != bcVARG {
			fs.pc--
			return insAD(call.op()-bcCALL+bcCALLT, call.a(), call.c())
		}

		fs.bc[e.info].ins.setB(0) // Append all the results of the call.

		return insAD(bcRETM, fs.nactvar(), e.aux-fs.nactvar())
	}

	if len(s.Values) == 1 { // Return the result from any register.
		return insAD(bcRET1, fs.toAnyReg(&e), 2)
	}

	fs.toNextReg(&e) // Force contiguous registers.

	return insAD(bcRET, fs.nactvar(), uint32(len(s.Values)+1))
}

// label parses a label and the labels right after it (parse_label).
func (p *parser) label(stmts *[]ast.Stmt) {
	fs := p.fs
	fs.lasttarget = fs.pc
	fs.bl.flags |= scopeGola
	pos := p.tok.pos

	p.next()

	name := p.name()
	if p.findLabel(name) != nil {
		p.failAt(p.lx.line, fmt.Sprintf(errDupLabel, name))
	}

	idx := p.golaNew(name, false, fs.pc)

	p.check(tkLabel)
	*stmts = append(*stmts, &ast.Label{At: pos, Name: name})

	for p.tok.typ == tkLabel {
		p.enter()
		p.label(stmts)
		p.leave()
	}

	// A label at the end of a block is outside the scope of its locals.
	if isBlockEnd(p.tok.typ) && p.tok.typ != tkUntil {
		p.gola[idx].slot = fs.bl.nactvar
	}

	p.golaResolve(fs.bl, idx, &p.gola[idx])
}

// lhsVar is a target of an assignment (LHSVarList).
type lhsVar struct {
	v    expDesc
	prev *lhsVar
}

// callOrAssign parses a call statement or an assignment
// (parse_call_assign).
func (p *parser) callOrAssign() ast.Stmt {
	var lh lhsVar

	pos := p.tok.pos
	e := p.primary(&lh.v)

	if lh.v.k == vCall {
		p.fs.bc[lh.v.info].ins.setB(1) // No results.
		return &ast.CallStmt{At: pos, Call: e}
	}

	s := &ast.Assign{At: pos, Targets: []ast.Expr{e}}
	p.assignment(s, &lh, 1)

	return s
}

// assignment parses the rest of an assignment: the targets after lh and
// the values (parse_assignment).
func (p *parser) assignment(s *ast.Assign, lh *lhsVar, nvars int) {
	var e expDesc

	fs := p.fs

	if lh.v.k < vLocal || lh.v.k > vIndexed {
		p.failNear(errSyntax)
	}

	if p.opt(',') { // Collect the targets and recurse.
		vl := &lhsVar{prev: lh}
		s.Targets = append(s.Targets, p.primary(&vl.v))

		if vl.v.k == vLocal {
			p.assignHazard(lh, &vl.v)
		}

		if p.level+nvars >= maxLevels {
			p.limit(fs, maxLevels, "variable names")
		}

		p.assignment(s, vl, nvars+1)
	} else {
		p.check('=')
		s.Values = p.exprList(&e)

		if len(s.Values) == nvars {
			if e.k == vCall {
				if fs.bc[e.info].ins.op() == bcVARG { // A vararg assignment.
					fs.freereg--
					e.k = vRelocable
				} else { // The base of a call is not relocable.
					e.info = e.aux
					e.k = vNonReloc
				}
			}

			fs.store(&lh.v, &e)

			return
		}

		p.assignAdjust(nvars, len(s.Values), &e)
	}

	// Assign the value to the target and return to the previous one.
	e.init(vNonReloc, fs.freereg-1)
	fs.store(&lh.v, &e)
}

// assignHazard renames the tables and keys of indexed targets that an
// assignment to a local overwrites before they are used (assign_hazard).
func (p *parser) assignHazard(lh *lhsVar, v *expDesc) {
	fs := p.fs
	reg := v.info
	tmp := fs.freereg
	hazard := false

	for ; lh != nil; lh = lh.prev {
		if lh.v.k != vIndexed {
			continue
		}

		if lh.v.info == reg { // t[i], t = 1, 2
			hazard = true
			lh.v.info = tmp
		}

		if lh.v.aux == reg { // t[i], i = 1, 2
			hazard = true
			lh.v.aux = tmp
		}
	}

	if hazard {
		fs.emit(insAD(bcMOV, tmp, reg))
		fs.regReserve(1)
	}
}

// body parses function parameters and body (parse_body). The line is
// the line the function is defined at, used in error messages. The
// function is a constant of the enclosing one, created with FNEW.
func (p *parser) body(e *expDesc, pos ast.Pos, needSelf bool, line int) *ast.Function {
	f := &ast.Function{At: pos}
	pfs := p.fs
	fs := p.openFunc(line)

	f.Params = p.params(pos, needSelf)
	fs.numparams = uint32(len(f.Params))
	fs.emit(insAD(bcFUNCF, 0, 0)) // A placeholder for the header.
	f.Body = p.chunk()

	if p.tok.typ != tkEnd {
		p.match(tkEnd, tkFunction, line)
	}

	f.Vararg = fs.flags&protoVararg != 0
	p.lastLine = p.lx.line
	pt := p.closeFunc()

	e.init(vRelocable, pfs.emit(insAD(bcFNEW, 0, pfs.constGC(gcConst{kind: "proto", proto: pt}))))

	if pfs.flags&protoChild == 0 {
		if pfs.flags&protoHasReturn != 0 {
			pfs.flags |= protoFixupReturn
		}

		pfs.flags |= protoChild
	}

	p.next() // Skip "end".

	return f
}

// params parses a parameter list (parse_params).
func (p *parser) params(pos ast.Pos, needSelf bool) []*ast.Local {
	var list []*ast.Local

	fs := p.fs

	p.check('(')

	if needSelf {
		self := &ast.Local{Name: "self", Decl: pos}
		p.declare(self)
		list = append(list, self)
	}

	if p.tok.typ != ')' {
		list = p.paramNames(list)
	}

	p.activate(len(list))
	fs.regReserve(uint32(len(list)))
	p.check(')')

	return list
}

// paramNames parses the names of the parameters and "...".
func (p *parser) paramNames(list []*ast.Local) []*ast.Local {
	for {
		switch {
		case p.isName():
			v := &ast.Local{Decl: p.tok.pos}
			v.Name = p.name()
			p.declare(v)
			list = append(list, v)
		case p.tok.typ == tkDots:
			p.next()
			p.fs.flags |= protoVararg

			return list
		default:
			p.failNear(errParam)
		}

		if !p.opt(',') {
			return list
		}
	}
}
