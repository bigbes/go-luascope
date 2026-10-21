package parser

import (
	"math"
	"math/bits"

	"github.com/bigbes/go-luascope/ast"
)

// binaryOp is a binary operator with its priorities (ORDER OPR).
type binaryOp struct {
	op          string
	opr         binOpr
	left, right int
}

var binaryOps = map[tokenType]binaryOp{
	'+': {"+", binAdd, 6, 6}, '-': {"-", binSub, 6, 6},
	'*': {"*", binMul, 7, 7}, '/': {"/", binDiv, 7, 7}, '%': {"%", binMod, 7, 7},
	'^':      {"^", binPow, 10, 9},    // Right associative.
	tkConcat: {"..", binConcat, 5, 4}, // Right associative.
	tkEq:     {"==", binEq, 3, 3}, tkNe: {"~=", binNe, 3, 3},
	'<': {"<", binLt, 3, 3}, tkLe: {"<=", binLe, 3, 3},
	'>': {">", binGt, 3, 3}, tkGe: {">=", binGe, 3, 3},
	tkAnd: {tkAnd.String(), binAnd, 2, 2},
	tkOr:  {tkOr.String(), binOr, 1, 1},
}

// unaryOp is a unary operator and its opcode.
type unaryOp struct {
	op string
	bc bcOp
}

var unaryOps = map[tokenType]unaryOp{
	tkNot: {tkNot.String(), bcNOT}, '-': {"-", bcUNM}, '#': {"#", bcLEN},
}

// unaryPriority is the priority of unary operators.
const unaryPriority = 8

// expr parses an expression (expr).
func (p *parser) expr(v *expDesc) ast.Expr {
	return p.binary(v, 0)
}

// exprNext parses an expression into the next register (expr_next).
func (p *parser) exprNext() ast.Expr {
	var e expDesc

	x := p.expr(&e)
	p.fs.toNextReg(&e)

	return x
}

// exprList parses a comma-separated list of expressions and leaves the
// last one in v (expr_list).
func (p *parser) exprList(v *expDesc) []ast.Expr {
	list := []ast.Expr{p.expr(v)}

	for p.opt(',') {
		p.fs.toNextReg(v)
		list = append(list, p.expr(v))
	}

	return list
}

// binary parses binary operations with a priority higher than the limit
// (expr_binop).
func (p *parser) binary(v *expDesc, limit int) ast.Expr {
	p.enter()

	e := p.unary(v)

	for {
		op, ok := binaryOps[p.tok.typ]
		if !ok || op.left <= limit {
			break
		}

		p.next()
		p.fs.binopLeft(op.opr, v)

		var v2 expDesc

		r := p.binary(&v2, op.right)
		p.fs.binop(op.opr, v, &v2)
		e = &ast.Binary{At: e.Pos(), Op: op.op, L: e, R: r}
	}

	p.leave()

	return e
}

// unary parses a unary operation or a simple expression (expr_unop).
func (p *parser) unary(v *expDesc) ast.Expr {
	op, ok := unaryOps[p.tok.typ]
	if !ok {
		return p.simple(v)
	}

	pos := p.tok.pos
	p.next()

	x := p.binary(v, unaryPriority)
	p.fs.unop(op.bc, v)

	return &ast.Unary{At: pos, Op: op.op, X: x}
}

// simple parses a literal, a constructor, a function or a primary
// expression (expr_simple).
func (p *parser) simple(v *expDesc) ast.Expr {
	tok := p.tok

	var e ast.Expr

	switch tok.typ {
	case tkNumber:
		if tok.cdata {
			v.init(vKCdata, 0)
		} else {
			v.init(vKNum, 0)
			v.nval = tok.num
		}

		e = &ast.Number{At: tok.pos, Raw: tok.val}
	case tkString:
		v.init(vKStr, 0)
		v.sval = tok.val
		e = &ast.String{At: tok.pos, Value: tok.val}
	case tkNil:
		v.init(vKNil, 0)
		e = &ast.Nil{At: tok.pos}
	case tkTrue:
		v.init(vKTrue, 0)
		e = &ast.True{At: tok.pos}
	case tkFalse:
		v.init(vKFalse, 0)
		e = &ast.False{At: tok.pos}
	case tkDots:
		fs := p.fs
		if fs.flags&protoVararg == 0 {
			p.failNear(errDots)
		}

		fs.regReserve(1)
		base := fs.freereg - 1
		v.init(vCall, fs.emit(insABC(bcVARG, base, 2, fs.numparams)))
		v.aux = base
		e = &ast.Vararg{At: tok.pos}
	case '{':
		return p.table(v)
	case tkFunction:
		p.next()
		return p.body(v, tok.pos, false, p.lx.line)
	default:
		return p.primary(v)
	}

	p.next()

	return e
}

// primary parses a name or a parenthesized expression followed by
// fields, indexes and calls (expr_primary).
func (p *parser) primary(v *expDesc) ast.Expr {
	var e ast.Expr

	fs := p.fs
	pos := p.tok.pos

	switch {
	case p.tok.typ == '(':
		line := p.lx.line
		p.next()
		x := p.expr(v)
		p.match(')', '(', line)
		fs.discharge(v)
		e = &ast.Paren{At: pos, X: x}
	case p.isName():
		name := p.name()
		e = &ast.Name{At: pos, Name: name, Local: p.lookup(name, v)}
	default:
		p.failNear(errSymbol)
	}

	for {
		switch p.tok.typ {
		case '.':
			e = p.field(e, v)
		case '[':
			var key expDesc

			fs.toAnyReg(v)
			k := p.bracket(&key)
			fs.index(v, &key)
			e = &ast.Index{At: pos, X: e, Key: k}
		case ':':
			p.next()

			method := &ast.String{At: p.tok.pos}
			method.Value = p.name()
			fs.method(v, method.Value)
			e = &ast.MethodCall{At: pos, Recv: e, Method: method, Args: p.args(v)}
		case '(', tkString, '{':
			fs.toNextReg(v)
			fs.regReserve(fr2)
			e = &ast.Call{At: pos, Func: e, Args: p.args(v)}
		default:
			return e
		}
	}
}

// field parses ".name" or ":name" (expr_field).
func (p *parser) field(x ast.Expr, v *expDesc) ast.Expr {
	var k expDesc

	p.fs.toAnyReg(v)
	p.next()

	key := &ast.String{At: p.tok.pos}
	key.Value = p.name()
	k.init(vKStr, 0)
	k.sval = key.Value
	p.fs.index(v, &k)

	return &ast.Index{At: x.Pos(), X: x, Key: key}
}

// bracket parses "[expr]" (expr_bracket).
func (p *parser) bracket(v *expDesc) ast.Expr {
	p.next()
	e := p.expr(v)
	p.fs.toVal(v)
	p.check(']')

	return e
}

// args parses call arguments and emits the call (parse_args).
func (p *parser) args(e *expDesc) []ast.Expr {
	var (
		args expDesc
		list []ast.Expr
		ins  bcIns
		fs   = p.fs
		line = p.lx.line
	)

	switch p.tok.typ {
	case '(':
		if line != p.lastLine {
			p.failNear(errAmbiguous)
		}

		p.next()

		if p.tok.typ == ')' {
			args.k = vVoid
		} else {
			list = p.exprList(&args)
			if args.k == vCall { // f(a, b, g()) or f(a, b, ...).
				fs.bc[args.info].ins.setB(0) // Pass on multiple results.
			}
		}

		p.match(')', '(', line)
	case '{':
		list = []ast.Expr{p.table(&args)}
	case tkString:
		args.init(vKStr, 0)
		args.sval = p.tok.val
		list = []ast.Expr{&ast.String{At: p.tok.pos, Value: p.tok.val}}
		p.next()
	default:
		p.failNear(errFuncArgs)
	}

	base := e.info // The base register of the call.
	if args.k == vCall {
		ins = insABC(bcCALLM, base, 2, args.aux-base-1-fr2)
	} else {
		if args.k != vVoid {
			fs.toNextReg(&args)
		}

		ins = insABC(bcCALL, base, 2, fs.freereg-base-fr2)
	}

	e.init(vCall, fs.emit(ins))
	e.aux = base
	fs.bc[fs.pc-1].line = int32(line)
	fs.freereg = base + 1 // Leave one result by default.

	return list
}

// hsize2hbits returns the number of hash bits of a table for n keys.
func hsize2hbits(n uint32) uint32 {
	switch n {
	case 0:
		return 0
	case 1:
		return 1
	default:
		return uint32(bits.Len32(n - 1))
	}
}

// tableCons is the state of a table constructor (expr_table).
type tableCons struct {
	e        *expDesc
	pc       uint32 // The TNEW or TDUP.
	freg     uint32 // The register after the table.
	template bool   // A template table holds the constant items.
	vcall    bool   // The last item is a call or "..." in the array part.
	needarr  bool
	narr     uint32 // The next array index.
	nhash    uint32
}

// table parses a table constructor (expr_table). Constant keys with
// constant values go to a template table instead of instructions.
func (p *parser) table(e *expDesc) *ast.Table {
	fs := p.fs
	line := p.lx.line
	t := &ast.Table{At: p.tok.pos}
	c := &tableCons{e: e, freg: fs.freereg, narr: 1}

	c.pc = fs.emit(insAD(bcTNEW, c.freg, 0))
	e.init(vNonReloc, c.freg)
	fs.regReserve(1)
	c.freg++

	p.check('{')

	for p.tok.typ != '}' {
		t.Fields = append(t.Fields, p.tableItem(c))
		fs.freereg = c.freg

		if !p.opt(',') && !p.opt(';') {
			break
		}
	}

	p.match('}', '{', line)
	p.tableEnd(c)

	return t
}

// tableItem parses an item of a table constructor.
func (p *parser) tableItem(c *tableCons) *ast.Field {
	var key, val expDesc

	fs := p.fs
	f := &ast.Field{Key: p.tableKey(c, &key)}
	f.Value = p.expr(&val)

	if key.isK() && key.k != vKNil && (key.k == vKStr || val.isKNoJump()) {
		if !c.template { // Create the template table on demand.
			c.template = true
			kidx := fs.constGC(gcConst{kind: "table"})
			fs.bc[c.pc].ins = insAD(bcTDUP, c.freg-1, kidx)
		}

		c.vcall = false

		if val.isKNoJump() { // The template table holds the key and the value.
			return f
		}
	}

	if val.k != vCall {
		fs.toAnyReg(&val)
		c.vcall = false
	}

	if key.isK() {
		fs.index(c.e, &key)
	}

	fs.store(c.e, &val)

	return f
}

// tableKey parses the key of a table item, "[expr] =" or "name =", or
// makes the next array index the key.
func (p *parser) tableKey(c *tableCons, key *expDesc) ast.Expr {
	var k ast.Expr

	c.vcall = false

	switch {
	case p.tok.typ == '[':
		k = p.bracket(key)
		if !key.isK() {
			p.fs.index(c.e, key)
		}

		if key.isNumK() && key.nval == 0 {
			c.needarr = true
		} else {
			c.nhash++
		}

		p.check('=')
	case p.isName() && p.lookahead() == '=':
		name := &ast.String{At: p.tok.pos}
		name.Value = p.name()
		k = name
		key.init(vKStr, 0)
		key.sval = name.Value
		p.check('=')
		c.nhash++
	default:
		key.init(vKNum, 0)
		key.nval = float64(c.narr)
		c.narr++
		c.needarr, c.vcall = true, true
	}

	return k
}

// tableEnd stores all the results of a call or "..." at the end of the
// array part and sets the sizes of the table.
func (p *parser) tableEnd(c *tableCons) {
	fs := p.fs

	if c.vcall {
		i := fs.pc - 1
		if c.narr > 256 { // Drop the TSETV, replace the load of its key.
			fs.pc--
			i--
		}

		biased := math.Float64frombits(0x43300000<<32 | uint64(c.narr-1)) // A biased integer.
		fs.bc[i].ins = insAD(bcTSETM, c.freg, fs.constNum(biased))
		fs.bc[i-1].ins.setB(0)
	}

	if c.pc == fs.pc-1 { // Make the expression relocable if possible.
		c.e.info = c.pc
		fs.freereg--
		c.e.k = vRelocable
	} else {
		c.e.k = vNonReloc // May have been changed by index.
	}

	if c.template {
		return
	}

	narr := c.narr // TNEW D: the hash bits and the array size.

	switch {
	case !c.needarr:
		narr = 0
	case narr < 3:
		narr = 3
	case narr > 0x7ff:
		narr = 0x7ff
	}

	fs.bc[c.pc].ins.setD(narr | hsize2hbits(c.nhash)<<11)
}
