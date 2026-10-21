package parser_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bigbes/go-luascope/ast"
	"github.com/bigbes/go-luascope/parser"
)

// printer renders a syntax tree as S-expressions. A local is printed with
// the number of its declaration, "x#2", so a reference shows which local
// it resolves to; a global is printed without one. A reference to a local
// that has not been declared yet is printed as "x#?".
type printer struct {
	b      strings.Builder
	locals map[*ast.Local]int
}

func render(t *testing.T, src string) string {
	t.Helper()

	chunk, err := parser.ParseBytes([]byte(src), "=test")
	require.NoError(t, err, src)

	p := &printer{locals: map[*ast.Local]int{}}
	p.stmts(chunk.Block)

	return p.b.String()
}

func (p *printer) printf(format string, args ...any) { fmt.Fprintf(&p.b, format, args...) }

func (p *printer) declare(v *ast.Local) {
	p.locals[v] = len(p.locals) + 1
	p.local(v)
}

func (p *printer) local(v *ast.Local) {
	if n, ok := p.locals[v]; ok {
		p.printf("%s#%d", v.Name, n)
	} else {
		p.printf("%s#?", v.Name)
	}
}

func (p *printer) declareList(vs []*ast.Local) {
	p.printf("(")

	for i, v := range vs {
		if i > 0 {
			p.printf(" ")
		}

		p.declare(v)
	}

	p.printf(")")
}

func (p *printer) stmts(b *ast.Block) {
	for i, s := range b.Stmts {
		if i > 0 {
			p.printf(" ")
		}

		p.stmt(s)
	}
}

func (p *printer) block(b *ast.Block) {
	p.printf("(")
	p.stmts(b)
	p.printf(")")
}

func (p *printer) exprs(es []ast.Expr) {
	for _, e := range es {
		p.printf(" ")
		p.expr(e)
	}
}

func (p *printer) list(es []ast.Expr) {
	p.printf("(")

	for i, e := range es {
		if i > 0 {
			p.printf(" ")
		}

		p.expr(e)
	}

	p.printf(")")
}

func (p *printer) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.LocalStmt:
		// The values are printed first: they are evaluated before the
		// locals are declared.
		p.printf("(local ")
		p.list(s.Values)
		p.printf(" ")
		p.declareList(s.Names)
		p.printf(")")
	case *ast.LocalFunction:
		p.printf("(localfunc ")
		p.declare(s.Name)
		p.printf(" ")
		p.expr(s.Func)
		p.printf(")")
	case *ast.Assign:
		p.printf("(assign ")
		p.list(s.Targets)
		p.printf(" ")
		p.list(s.Values)
		p.printf(")")
	case *ast.CallStmt:
		p.expr(s.Call)
	case *ast.Do:
		p.printf("(do ")
		p.block(s.Body)
		p.printf(")")
	case *ast.While:
		p.printf("(while ")
		p.expr(s.Cond)
		p.printf(" ")
		p.block(s.Body)
		p.printf(")")
	case *ast.Repeat:
		p.printf("(repeat ")
		p.block(s.Body)
		p.printf(" ")
		p.expr(s.Cond)
		p.printf(")")
	case *ast.If:
		p.printf("(if")

		for i, c := range s.Conds {
			p.printf(" ")
			p.expr(c)
			p.printf(" ")
			p.block(s.Blocks[i])
		}

		if s.Else != nil {
			p.printf(" else ")
			p.block(s.Else)
		}

		p.printf(")")
	case *ast.NumericFor:
		p.printf("(for-num")
		p.exprs([]ast.Expr{s.Start, s.Limit})

		if s.Step != nil {
			p.exprs([]ast.Expr{s.Step})
		}

		p.printf(" ")
		p.declare(s.Var)
		p.printf(" ")
		p.block(s.Body)
		p.printf(")")
	case *ast.GenericFor:
		p.printf("(for-in ")
		p.list(s.Exprs)
		p.printf(" ")
		p.declareList(s.Vars)
		p.printf(" ")
		p.block(s.Body)
		p.printf(")")
	case *ast.FunctionStmt:
		p.printf("(funcstmt ")
		p.expr(s.Target)

		if s.Method {
			p.printf(" method")
		}

		p.printf(" ")
		p.expr(s.Func)
		p.printf(")")
	case *ast.Return:
		p.printf("(return")
		p.exprs(s.Values)
		p.printf(")")
	case *ast.Break:
		p.printf("(break)")
	case *ast.Goto:
		p.printf("(goto %s)", s.Label)
	case *ast.Label:
		p.printf("(label %s)", s.Name)
	default:
		p.printf("(unknown %T)", s)
	}
}

func (p *printer) expr(e ast.Expr) {
	switch e := e.(type) {
	case *ast.Nil:
		p.printf("nil")
	case *ast.True:
		p.printf("true")
	case *ast.False:
		p.printf("false")
	case *ast.Vararg:
		p.printf("...")
	case *ast.Number:
		p.printf("%s", e.Raw)
	case *ast.String:
		p.printf("%s", strconv.Quote(e.Value))
	case *ast.Name:
		if e.Local == nil {
			p.printf("%s", e.Name)
		} else {
			p.local(e.Local)
		}
	case *ast.Index:
		p.printf("(index ")
		p.expr(e.X)
		p.printf(" ")
		p.expr(e.Key)
		p.printf(")")
	case *ast.Call:
		p.printf("(call ")
		p.expr(e.Func)
		p.exprs(e.Args)
		p.printf(")")
	case *ast.MethodCall:
		p.printf("(method ")
		p.expr(e.Recv)
		p.printf(" ")
		p.expr(e.Method)
		p.exprs(e.Args)
		p.printf(")")
	case *ast.Function:
		p.printf("(function ")
		p.declareList(e.Params)

		if e.Vararg {
			p.printf(" vararg")
		}

		p.printf(" ")
		p.block(e.Body)
		p.printf(")")
	case *ast.Table:
		p.printf("(table")

		for _, f := range e.Fields {
			p.printf(" ")

			if f.Key != nil {
				p.printf("[")
				p.expr(f.Key)
				p.printf("]=")
			}

			p.expr(f.Value)
		}

		p.printf(")")
	case *ast.Binary:
		p.printf("(%s ", e.Op)
		p.expr(e.L)
		p.printf(" ")
		p.expr(e.R)
		p.printf(")")
	case *ast.Unary:
		p.printf("(%s ", e.Op)
		p.expr(e.X)
		p.printf(")")
	case *ast.Paren:
		p.printf("(paren ")
		p.expr(e.X)
		p.printf(")")
	default:
		p.printf("(unknown %T)", e)
	}
}

type treeTest struct {
	src  string
	want string
}

func checkTrees(t *testing.T, tests []treeTest) {
	t.Helper()

	for _, tt := range tests {
		require.Equal(t, tt.want, render(t, tt.src), tt.src)
	}
}

// TestTreeLiterals checks the values of literals: numbers as written,
// strings decoded.
func TestTreeLiterals(t *testing.T) {
	t.Parallel()

	checkTrees(t, []treeTest{
		{"return 0, 1, 42, 1.5, .5, 5., 1e10, 1E-3, 0x10, 0XfF, 0x1p4",
			"(return 0 1 42 1.5 .5 5. 1e10 1E-3 0x10 0XfF 0x1p4)"},
		{"return 0b101, 3LL, 2ULL, 1i, 0x10LL",
			"(return 0b101 3LL 2ULL 1i 0x10LL)"},
		{`return "a", 'b', "\65\x42\u{43}", "\n\t\\\"", "a\z
		      b", [[long]], [==[
x]]==]`,
			`(return "a" "b" "ABC" "\n\t\\\"" "ab" "long" "x]")`},
		{"return nil, true, false, ...", "(return nil true false ...)"},
		{"return {1, x = 2, [3] = 4; 'y'}", `(return (table 1 ["x"]=2 [3]=4 "y"))`},
		{"return function(a, ...) end", "(return (function (a#1) vararg ()))"},
	})
}

// TestTreeOperators checks that every operator is parsed as itself.
func TestTreeOperators(t *testing.T) {
	t.Parallel()

	var tests []treeTest

	for _, op := range []string{
		"+", "-", "*", "/", "%", "^", "..", "==", "~=", "<", "<=", ">", ">=", "and", "or",
	} {
		tests = append(tests, treeTest{"return a " + op + " b", "(return (" + op + " a b))"})
	}

	for _, op := range []string{"-", "not", "#"} {
		tests = append(tests, treeTest{"return " + op + " a", "(return (" + op + " a))"})
	}

	checkTrees(t, tests)
}

// TestTreePrecedence checks the priorities and the associativity of the
// operators, which are those of Lua 5.1.
func TestTreePrecedence(t *testing.T) {
	t.Parallel()

	checkTrees(t, []treeTest{
		{"return 1 + 2 * 3", "(return (+ 1 (* 2 3)))"},
		{"return 1 * 2 + 3", "(return (+ (* 1 2) 3))"},
		{"return a / b % c * d", "(return (* (% (/ a b) c) d))"},
		{"return a - b - c", "(return (- (- a b) c))"},
		{"return a .. b .. c", "(return (.. a (.. b c)))"},
		{"return a ^ b ^ c", "(return (^ a (^ b c)))"},
		{"return a + b .. c + d", "(return (.. (+ a b) (+ c d)))"},
		{"return a .. b == c", "(return (== (.. a b) c))"},
		{"return a < b == c", "(return (== (< a b) c))"},
		{"return a == b and c ~= d or e", "(return (or (and (== a b) (~= c d)) e))"},
		{"return a or b and c", "(return (or a (and b c)))"},
		{"return a and b or c", "(return (or (and a b) c))"},
		{"return -a ^ b", "(return (- (^ a b)))"},
		{"return -a * b", "(return (* (- a) b))"},
		{"return 2 ^ -3", "(return (^ 2 (- 3)))"},
		{"return not a == b", "(return (== (not a) b))"},
		{"return - - #a", "(return (- (- (# a))))"},
		{"return #t + 1", "(return (+ (# t) 1))"},
		{"return (a + b) * c", "(return (* (paren (+ a b)) c))"},
		{"return a.b.c:d(e)[f]", `(return (index (method (index (index a "b") "c") "d" e) f))`},
		{"f{1} 'x' (y)", `(call (call (call f (table 1)) "x") y)`},
	})
}

// TestTreeLocals checks which local or global every name resolves to.
func TestTreeLocals(t *testing.T) {
	t.Parallel()

	checkTrees(t, []treeTest{
		// A local is visible after its statement, until the end of its block.
		{"local x = 1 do local x = x return x end return x",
			"(local (1) (x#1)) (do ((local (x#1) (x#2)) (return x#2))) (return x#1)"},
		{"local x = x", "(local (x) (x#1))"},
		{"local a, a = 1, 2 return a", "(local (1 2) (a#1 a#2)) (return a#2)"},
		// A local function sees itself, a local assigned a function does not.
		{"local function f() return f end", "(localfunc f#1 (function () ((return f#1))))"},
		{"local f = function() return f end", "(local ((function () ((return f)))) (f#1))"},
		// The condition of repeat sees the locals of the body.
		{"repeat local y = 1 until y", "(repeat ((local (1) (y#1))) y#1)"},
		{"repeat local y = 1 until z return y",
			"(repeat ((local (1) (y#1))) z) (return y)"},
		// The loop variables are visible in the body only.
		{"for i = 1, i do local i = i end return i",
			"(for-num 1 i i#1 ((local (i#1) (i#2)))) (return i)"},
		{"for k, v in next, k do return k, v end",
			"(for-in (next k) (k#1 v#2) ((return k#1 v#2)))"},
		// A method has the implicit parameter self.
		{"function t.a:m(x) return self, x end",
			`(funcstmt (index (index t "a") "m") method ` +
				`(function (self#1 x#2) ((return self#1 x#2))))`},
		{"function t.m(x) return self, x end",
			`(funcstmt (index t "m") (function (x#1) ((return self x#1))))`},
		// An upvalue is the local of the enclosing function.
		{"local u function f() return function() return u end end",
			"(local () (u#1)) (funcstmt f (function () ((return (function () ((return u#1)))))))"},
		{"local function g(p) local q return function(r) return p, q, r end end",
			"(localfunc g#1 (function (p#2) ((local () (q#3)) " +
				"(return (function (r#4) ((return p#2 q#3 r#4)))))))"},
		// Each branch is a block.
		{"if a then local b = 1 elseif b then local c else return c end",
			"(if a ((local (1) (b#1))) b ((local () (c#2))) else ((return c)))"},
		{"while x do local x = x end", "(while x ((local (x) (x#1))))"},
	})
}
