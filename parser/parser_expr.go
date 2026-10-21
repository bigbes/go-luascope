package parser

import "github.com/bigbes/go-luascope/ast"

// binaryOp is a binary operator with its priorities (ORDER OPR).
type binaryOp struct {
	op          string
	left, right int
}

var binaryOps = map[tokenType]binaryOp{
	'+': {"+", 6, 6}, '-': {"-", 6, 6},
	'*': {"*", 7, 7}, '/': {"/", 7, 7}, '%': {"%", 7, 7},
	'^':      {"^", 10, 9}, // Right associative.
	tkConcat: {"..", 5, 4}, // Right associative.
	tkEq:     {"==", 3, 3}, tkNe: {"~=", 3, 3},
	'<': {"<", 3, 3}, tkLe: {"<=", 3, 3}, '>': {">", 3, 3}, tkGe: {">=", 3, 3},
	tkAnd: {tkAnd.String(), 2, 2},
	tkOr:  {tkOr.String(), 1, 1},
}

var unaryOps = map[tokenType]string{tkNot: tkNot.String(), '-': "-", '#': "#"}

// unaryPriority is the priority of unary operators.
const unaryPriority = 8

// expr parses an expression.
func (p *parser) expr() ast.Expr {
	return p.binary(0)
}

// exprList parses a comma-separated list of expressions.
func (p *parser) exprList() []ast.Expr {
	list := []ast.Expr{p.expr()}
	for p.opt(',') {
		list = append(list, p.expr())
	}

	return list
}

// binary parses binary operations with a priority higher than the limit
// (expr_binop).
func (p *parser) binary(limit int) ast.Expr {
	p.enter()

	e := p.unary()

	for {
		op, ok := binaryOps[p.tok.typ]
		if !ok || op.left <= limit {
			break
		}

		p.next()
		e = &ast.Binary{At: e.Pos(), Op: op.op, L: e, R: p.binary(op.right)}
	}

	p.leave()

	return e
}

// unary parses a unary operation or a simple expression (expr_unop).
func (p *parser) unary() ast.Expr {
	op, ok := unaryOps[p.tok.typ]
	if !ok {
		return p.simple()
	}

	pos := p.tok.pos
	p.next()

	return &ast.Unary{At: pos, Op: op, X: p.binary(unaryPriority)}
}

// simple parses a literal, a constructor, a function or a primary
// expression (expr_simple).
func (p *parser) simple() ast.Expr {
	tok := p.tok

	var e ast.Expr

	switch tok.typ {
	case tkNumber:
		e = &ast.Number{At: tok.pos, Raw: tok.val}
	case tkString:
		e = &ast.String{At: tok.pos, Value: tok.val}
	case tkNil:
		e = &ast.Nil{At: tok.pos}
	case tkTrue:
		e = &ast.True{At: tok.pos}
	case tkFalse:
		e = &ast.False{At: tok.pos}
	case tkDots:
		if !p.fs.vararg {
			p.failNear(errDots)
		}

		e = &ast.Vararg{At: tok.pos}
	case '{':
		return p.table()
	case tkFunction:
		p.next()
		return p.body(tok.pos, false, p.lx.line)
	default:
		return p.primary()
	}

	p.next()

	return e
}

// primary parses a name or a parenthesized expression followed by
// fields, indexes and calls (expr_primary).
func (p *parser) primary() ast.Expr {
	var e ast.Expr

	pos := p.tok.pos

	switch {
	case p.tok.typ == '(':
		line := p.lx.line
		p.next()
		x := p.expr()
		p.match(')', '(', line)
		e = &ast.Paren{At: pos, X: x}
	case p.isName():
		name := p.name()
		e = &ast.Name{At: pos, Name: name, Local: p.lookup(name)}
	default:
		p.failNear(errSymbol)
	}

	for {
		switch p.tok.typ {
		case '.':
			e = p.field(e)
		case '[':
			e = &ast.Index{At: pos, X: e, Key: p.bracket()}
		case ':':
			p.next()
			method := &ast.String{At: p.tok.pos}
			method.Value = p.name()
			e = &ast.MethodCall{At: pos, Recv: e, Method: method, Args: p.args()}
		case '(', tkString, '{':
			e = &ast.Call{At: pos, Func: e, Args: p.args()}
		default:
			return e
		}
	}
}

// field parses ".name" or ":name" (expr_field).
func (p *parser) field(x ast.Expr) ast.Expr {
	p.next()

	key := &ast.String{At: p.tok.pos}
	key.Value = p.name()

	return &ast.Index{At: x.Pos(), X: x, Key: key}
}

// bracket parses "[expr]" (expr_bracket).
func (p *parser) bracket() ast.Expr {
	p.next()
	e := p.expr()
	p.check(']')

	return e
}

// args parses call arguments (parse_args).
func (p *parser) args() []ast.Expr {
	line := p.lx.line

	switch p.tok.typ {
	case '(':
		if line != p.lastLine {
			p.failNear(errAmbiguous)
		}

		p.next()

		var args []ast.Expr
		if p.tok.typ != ')' {
			args = p.exprList()
		}

		p.match(')', '(', line)

		return args
	case '{':
		return []ast.Expr{p.table()}
	case tkString:
		s := &ast.String{At: p.tok.pos, Value: p.tok.val}
		p.next()

		return []ast.Expr{s}
	default:
		p.failNear(errFuncArgs)
		return nil
	}
}

// table parses a table constructor (expr_table).
func (p *parser) table() *ast.Table {
	t := &ast.Table{At: p.tok.pos}
	line := p.lx.line

	p.check('{')

	for p.tok.typ != '}' {
		f := &ast.Field{}

		switch {
		case p.tok.typ == '[':
			f.Key = p.bracket()
			p.check('=')
		case p.isName() && p.lookahead() == '=':
			key := &ast.String{At: p.tok.pos}
			key.Value = p.name()
			f.Key = key
			p.check('=')
		}

		f.Value = p.expr()
		t.Fields = append(t.Fields, f)

		if !p.opt(',') && !p.opt(';') {
			break
		}
	}

	p.match('}', '{', line)

	return t
}

// body parses function parameters and body (parse_body). The line is
// the line the function is defined at, used in error messages.
func (p *parser) body(pos ast.Pos, needSelf bool, line int) *ast.Function {
	f := &ast.Function{At: pos}
	fs := p.openFunc(line)

	p.check('(')

	if needSelf {
		self := &ast.Local{Name: "self", Decl: pos}
		p.declare(self)
		f.Params = append(f.Params, self)
	}

	if p.tok.typ != ')' {
		f.Params = p.params(f.Params)
	}

	p.activate(len(f.Params))
	p.check(')')
	f.Body = p.chunk()

	if p.tok.typ != tkEnd {
		p.match(tkEnd, tkFunction, line)
	}

	f.Vararg = fs.vararg
	p.closeFunc()
	p.next() // Skip "end".

	return f
}

// params parses a parameter list (parse_params).
func (p *parser) params(list []*ast.Local) []*ast.Local {
	for {
		switch {
		case p.isName():
			v := &ast.Local{Decl: p.tok.pos}
			v.Name = p.name()
			p.declare(v)
			list = append(list, v)
		case p.tok.typ == tkDots:
			p.next()
			p.fs.vararg = true

			return list
		default:
			p.failNear(errParam)
		}

		if !p.opt(',') {
			return list
		}
	}
}
