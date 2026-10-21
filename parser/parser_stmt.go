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
		// Any statement but "do" emits instructions after a UCLO, so only
		// a "do" block keeps it as the last instruction.
		isDo := p.tok.typ == tkDo
		if p.stmtUclo = p.uclo; !isDo {
			p.uclo = 0
		}

		last = p.statement(&b.Stmts)

		if !isDo {
			p.uclo = 0
		}

		p.opt(';')
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
		p.fs.bl.flags |= scopeBreak
		p.golaNew(breakLabel, true, p.jumpLine())
		*stmts = append(*stmts, &ast.Break{At: pos})

		return true // Must be last in Lua 5.1.
	case p.tok.typ == tkGoto && p.lookahead() == tkName:
		p.next()
		label := p.name()
		p.fs.bl.flags |= scopeGola
		p.golaNew(label, true, p.jumpLine())
		*stmts = append(*stmts, &ast.Goto{At: pos, Label: label})
	default:
		*stmts = append(*stmts, p.callOrAssign())
	}

	return false
}

func (p *parser) ifStmt(line int) *ast.If {
	s := &ast.If{At: p.tok.pos}

	for {
		p.next() // Skip "if" or "elseif".
		s.Conds = append(s.Conds, p.expr())
		p.check(tkThen)
		s.Blocks = append(s.Blocks, p.block())

		if p.tok.typ != tkElseif {
			break
		}
	}

	if p.opt(tkElse) {
		s.Else = p.block()
	}

	p.match(tkEnd, tkIf, line)

	return s
}

func (p *parser) whileStmt(line int) *ast.While {
	s := &ast.While{At: p.tok.pos}

	p.next()
	s.Cond = p.expr()
	p.openScope(scopeLoop)
	p.check(tkDo)
	s.Body = p.block()
	p.match(tkEnd, tkWhile, line)
	p.closeScope()

	return s
}

func (p *parser) repeatStmt(line int) *ast.Repeat {
	s := &ast.Repeat{At: p.tok.pos}

	p.openScope(scopeLoop)
	p.openScope(0)
	p.next()
	s.Body = p.chunk()
	p.match(tkUntil, tkRepeat, line)
	s.Cond = p.expr() // Still inside the inner scope.
	p.closeScope()
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
		s = p.numericFor(pos, v)
	case ',', tkIn:
		s = p.genericFor(pos, v)
	default:
		p.failNear(errFor)
	}

	p.match(tkEnd, tkFor, line)
	p.closeScope()

	return s
}

// declareHidden declares the three hidden loop control variables.
func (p *parser) declareHidden() {
	for range 3 {
		p.declare(nil)
	}
}

func (p *parser) numericFor(pos ast.Pos, v *ast.Local) *ast.NumericFor {
	s := &ast.NumericFor{At: pos, Var: v}

	p.declareHidden()
	p.declare(v)
	p.check('=')
	s.Start = p.expr()
	p.check(',')
	s.Limit = p.expr()

	if p.opt(',') {
		s.Step = p.expr()
	}

	p.activate(3)
	p.check(tkDo)
	p.openScope(0)
	p.activate(1)
	s.Body = p.block()
	p.closeScope()

	return s
}

func (p *parser) genericFor(pos ast.Pos, first *ast.Local) *ast.GenericFor {
	s := &ast.GenericFor{At: pos, Vars: []*ast.Local{first}}

	p.declareHidden()
	p.declare(first)

	for p.opt(',') {
		v := &ast.Local{Decl: p.tok.pos}
		v.Name = p.name()
		p.declare(v)
		s.Vars = append(s.Vars, v)
	}

	p.check(tkIn)
	s.Exprs = p.exprList()
	p.activate(3)
	p.check(tkDo)
	p.openScope(0)
	p.activate(len(s.Vars))
	s.Body = p.block()
	p.closeScope()

	return s
}

func (p *parser) functionStmt(line int) *ast.FunctionStmt {
	s := &ast.FunctionStmt{At: p.tok.pos}

	p.next()

	pos := p.tok.pos
	name := p.name()

	var target ast.Expr = &ast.Name{At: pos, Name: name, Local: p.lookup(name)}

	for p.tok.typ == '.' {
		target = p.field(target)
	}

	if p.tok.typ == ':' {
		s.Method = true
		target = p.field(target)
	}

	s.Target = target
	s.Func = p.body(s.At, s.Method, line)

	return s
}

func (p *parser) localStmt(pos ast.Pos) ast.Stmt {
	if p.opt(tkFunction) {
		v := &ast.Local{Decl: p.tok.pos}
		v.Name = p.name()
		p.declare(v)
		p.activate(1)

		return &ast.LocalFunction{At: pos, Name: v, Func: p.body(pos, false, p.lx.line)}
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

	if p.opt('=') {
		s.Values = p.exprList()
	}

	p.activate(len(s.Names))

	return s
}

func (p *parser) returnStmt() *ast.Return {
	s := &ast.Return{At: p.tok.pos}

	p.next()

	if !isBlockEnd(p.tok.typ) && p.tok.typ != ';' {
		s.Values = p.exprList()
	}

	return s
}

// label parses a label and the labels right after it (parse_label).
func (p *parser) label(stmts *[]ast.Stmt) {
	fs := p.fs
	fs.bl.flags |= scopeGola
	pos := p.tok.pos

	p.next()

	name := p.name()
	if p.findLabel(name) {
		p.failAt(p.lx.line, fmt.Sprintf(errDupLabel, name))
	}

	idx := p.golaNew(name, false, p.lastLine)

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

// callOrAssign parses a call statement or an assignment.
func (p *parser) callOrAssign() ast.Stmt {
	pos := p.tok.pos
	e := p.primary()

	switch e.(type) {
	case *ast.Call, *ast.MethodCall:
		return &ast.CallStmt{At: pos, Call: e}
	}

	s := &ast.Assign{At: pos, Targets: []ast.Expr{e}}

	for {
		if !assignable(s.Targets[len(s.Targets)-1]) {
			p.failNear(errSyntax)
		}

		if !p.opt(',') {
			break
		}

		t := p.primary()
		if p.level+len(s.Targets) >= maxLevels {
			p.limit(p.fs, maxLevels, "variable names")
		}

		s.Targets = append(s.Targets, t)
	}

	p.check('=')
	s.Values = p.exprList()

	return s
}

func assignable(e ast.Expr) bool {
	switch e.(type) {
	case *ast.Name, *ast.Index:
		return true
	default:
		return false
	}
}
