// Package globals finds the global variables and modules Lua code uses.
//
// The analysis is static. Names are resolved by the parser with the rules
// of the LuaJIT compiler, so a global here is exactly a GGET or GSET in the
// bytecode. Field accesses are followed through constant keys:
// "box.space.users:select()" uses the path box.space.users.select.
package globals

import (
	"slices"
	"strconv"
	"strings"

	"github.com/bigbes/go-luascope/ast"
)

// Segment is an element of a path: a global name or a field name. A
// field indexed with a non-constant key is Dynamic and has no name.
type Segment struct {
	Name    string
	Dynamic bool
}

// Path is a global variable followed by the fields accessed through it.
type Path []Segment

// String formats the path like Lua code: box.space["my space"][?].
func (p Path) String() string {
	var b strings.Builder

	for i, s := range p {
		switch {
		case s.Dynamic:
			b.WriteString("[?]")
		case i == 0:
			b.WriteString(s.Name)
		case isIdent(s.Name):
			b.WriteString("." + s.Name)
		default:
			b.WriteString("[" + strconv.Quote(s.Name) + "]")
		}
	}

	return b.String()
}

func isIdent(s string) bool {
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		return false
	}

	for i := range len(s) {
		c := s[i] | 0x20
		if (c < 'a' || c > 'z') && (s[i] < '0' || s[i] > '9') && s[i] != '_' {
			return false
		}
	}

	return true
}

// Access is a read or a write of a global variable.
type Access struct {
	Path  Path
	Write bool
	Pos   ast.Pos
}

// Require is a call of the global require: "require(name)", or a protected
// call of it with the global pcall or xpcall: "pcall(require, name)" or
// "xpcall(require, handler, name)".
type Require struct {
	// Module is the module name if it is a constant.
	Module string
	// Dynamic is set when the module name is not a constant.
	Dynamic bool
	Pos     ast.Pos
}

// Usage lists the global variables and modules a chunk uses.
type Usage struct {
	Accesses []Access
	Requires []Require
	// RequireValues are uses of the global require other than calls
	// recorded in Requires, such as "local r = require" or "f(require, name)".
	RequireValues []ast.Pos
}

// RequireName is the name of the global require function.
const RequireName = "require"

// Analyze collects the global variables and modules used by a chunk.
// Every use of a global name produces exactly one Access. The names in the
// usage are strings of the tree, which keep the source of the chunk in
// memory, see parser.ParseBytes.
func Analyze(chunk *ast.Chunk) *Usage {
	a := &analyzer{}
	ast.Inspect(chunk.Block, a.visit)

	return &a.usage
}

type analyzer struct {
	usage Usage
}

func (a *analyzer) visit(node ast.Node) bool {
	switch n := node.(type) {
	case *ast.Name:
		if n.Local == nil {
			a.access(Path{{Name: n.Name}}, false, n.At)
		}
	case *ast.Index:
		a.chain(n, nil, false)
	case *ast.MethodCall:
		a.methodCall(n)
	case *ast.Call:
		return !a.requireCall(n)
	case *ast.Assign:
		for _, t := range n.Targets {
			a.target(t)
		}

		a.walk(n.Values...)
	case *ast.FunctionStmt:
		a.target(n.Target)
		a.walk(n.Func)
	default:
		return true
	}

	return false
}

func (a *analyzer) walk(nodes ...ast.Expr) {
	for _, n := range nodes {
		ast.Inspect(n, a.visit)
	}
}

func (a *analyzer) access(path Path, write bool, pos ast.Pos) {
	a.usage.Accesses = append(a.usage.Accesses, Access{Path: path, Write: write, Pos: pos})

	if !write && path[0].Name == RequireName {
		a.usage.RequireValues = append(a.usage.RequireValues, pos)
	}
}

// chain records the access through a chain of fields rooted at a global
// name. A method call adds its method to the path. A chain with any other
// root is not an access; its root is walked instead. The non-constant keys
// are walked in both cases. A chain is analyzed once, from its outermost
// field, so the analysis is linear in its length.
func (a *analyzer) chain(e ast.Expr, method *ast.String, write bool) {
	root, fields, keys := splitChain(e)

	if n, ok := root.(*ast.Name); ok && n.Local == nil {
		path := append(Path{{Name: n.Name}}, fields...)
		if method != nil {
			path = append(path, Segment{Name: method.Value})
		}

		a.access(path, write, n.At)
	} else {
		a.walk(root)
	}

	a.walk(keys...)
}

// splitChain splits a chain of fields and parentheses into its root, the
// fields after the root and the non-constant keys, both in source order.
// Like LuaJIT, a string constant in parentheses is a constant key:
// box[("schema")] is box.schema.
func splitChain(e ast.Expr) (ast.Expr, Path, []ast.Expr) {
	var (
		fields Path
		keys   []ast.Expr
	)

	for {
		switch x := e.(type) {
		case *ast.Index:
			if s, ok := unparen(x.Key).(*ast.String); ok {
				fields = append(fields, Segment{Name: s.Value})
			} else {
				fields = append(fields, Segment{Dynamic: true})
				keys = append(keys, x.Key)
			}

			e = x.X
		case *ast.Paren:
			e = x.X
		default:
			slices.Reverse(fields)
			slices.Reverse(keys)

			return e, fields, keys
		}
	}
}

func (a *analyzer) methodCall(c *ast.MethodCall) {
	a.chain(c.Recv, c.Method, false)
	a.walk(c.Args...)
}

// target records an assignment target.
func (a *analyzer) target(e ast.Expr) {
	if n, ok := e.(*ast.Name); ok {
		if n.Local == nil {
			a.access(Path{{Name: n.Name}}, true, n.At)
		}

		return
	}

	a.chain(e, nil, true)
}

// protectedCalls are the global functions that call their first argument,
// with the index of the first argument they pass to it.
var protectedCalls = map[string]int{"pcall": 1, "xpcall": 2}

// requireCall records a call of the global require, directly or through
// pcall or xpcall, and reports whether the call was one.
func (a *analyzer) requireCall(c *ast.Call) bool {
	if name, ok := globalName(c.Func, RequireName); ok {
		a.require(name, c.Args, 0)
		return true
	}

	callee, ok := unparen(c.Func).(*ast.Name)
	if !ok || callee.Local != nil || len(c.Args) == 0 {
		return false
	}

	arg, ok := protectedCalls[callee.Name]
	if !ok {
		return false
	}

	name, ok := globalName(c.Args[0], RequireName)
	if !ok {
		return false
	}

	a.access(Path{{Name: callee.Name}}, false, callee.At)
	a.require(name, c.Args[1:], arg-1)

	return true
}

// require records a require with the module name in args[i].
func (a *analyzer) require(name *ast.Name, args []ast.Expr, i int) {
	r := Require{Pos: name.At, Dynamic: true}
	if i < len(args) {
		var ok bool

		r.Module, ok = constString(args[i])
		r.Dynamic = !ok
	}

	a.usage.Requires = append(a.usage.Requires, r)
	a.usage.Accesses = append(a.usage.Accesses,
		Access{Path: Path{{Name: RequireName}}, Pos: name.At})
	a.walk(args...)
}

// globalName returns the global name e refers to if it is the given one.
func globalName(e ast.Expr, name string) (*ast.Name, bool) {
	n, ok := unparen(e).(*ast.Name)
	if !ok || n.Local != nil || n.Name != name {
		return nil, false
	}

	return n, true
}

func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.Paren)
		if !ok {
			return e
		}

		e = p.X
	}
}

// constString evaluates string literals and their concatenations.
func constString(e ast.Expr) (string, bool) {
	switch x := unparen(e).(type) {
	case *ast.String:
		return x.Value, true
	case *ast.Binary:
		if x.Op != ".." {
			return "", false
		}

		l, okL := constString(x.L)
		r, okR := constString(x.R)

		return l + r, okL && okR
	default:
		return "", false
	}
}
