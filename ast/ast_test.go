package ast_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bigbes/go-luascope/ast"
	"github.com/bigbes/go-luascope/parser"
)

func TestPosString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "3:15", ast.Pos{Line: 3, Col: 15}.String())
	assert.Equal(t, "1:1", ast.Pos{Line: 1, Col: 1}.String())
}

func TestPosOfNodes(t *testing.T) {
	t.Parallel()

	chunk, err := parser.ParseBytes([]byte("x = 1\n"), "test.lua")
	require.NoError(t, err)

	var sawName, sawNumber bool

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.Name:
			sawName = true
			assert.Equal(t, ast.Pos{Line: 1, Col: 1}, n.Pos())
		case *ast.Number:
			sawNumber = true
			assert.Equal(t, ast.Pos{Line: 1, Col: 5}, n.Pos())
		}

		return true
	})

	assert.True(t, sawName)
	assert.True(t, sawNumber)
}

// collectInspect returns the types of the nodes Inspect visits, in order.
func collectInspect(root ast.Node) []string {
	var types []string

	ast.Inspect(root, func(node ast.Node) bool {
		types = append(types, fmt.Sprintf("%T", node))
		return true
	})

	return types
}

func TestInspectOrder(t *testing.T) {
	t.Parallel()

	src := []byte("local t = {a = 1, \"b\"}\nfor i = 1, #t do\n  t[i] = -i\nend\n")
	chunk, err := parser.ParseBytes(src, "test.lua")
	require.NoError(t, err)

	types := collectInspect(chunk.Block)

	assert.Equal(t, []string{
		"*ast.Block",
		"*ast.LocalStmt", "*ast.Table", "*ast.String", "*ast.Number", "*ast.String",
		"*ast.NumericFor", "*ast.Number", "*ast.Unary", "*ast.Name",
		"*ast.Block", "*ast.Assign", "*ast.Index", "*ast.Name", "*ast.Name",
		"*ast.Unary", "*ast.Name",
	}, types)
}

func TestInspectStopsOnFalse(t *testing.T) {
	t.Parallel()

	chunk, err := parser.ParseBytes([]byte("x = f(1, 2)\n"), "test.lua")
	require.NoError(t, err)

	var types []string

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		if _, call := node.(*ast.Call); call {
			return false
		}

		types = append(types, fmt.Sprintf("%T", node))

		return true
	})

	// The call and its arguments are not visited.
	assert.Equal(t, []string{"*ast.Block", "*ast.Assign", "*ast.Name"}, types)
}

func TestInspectNil(t *testing.T) {
	t.Parallel()

	var called bool

	ast.Inspect(nil, func(ast.Node) bool {
		called = true
		return true
	})

	assert.False(t, called)
}

func TestInspectAllNodeKinds(t *testing.T) {
	t.Parallel()

	src := []byte(`local a = 1
local function f(...)
  do
    while true do break end
    repeat local b = 1 until b
    if a then return elseif not a then return a else return ... end
    for i = 1, 2, 3 do break end
  end
end
for k, v in pairs({}) do f(k, v) end
function t.m:call(x) return self, x end
function t:method() return (1 + 2) end
t:method(3)
goto done
::done::
x.y, z = 1, 2
f()
`)
	chunk, err := parser.ParseBytes(src, "test.lua")
	require.NoError(t, err)

	types := map[string]bool{}

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		types[fmt.Sprintf("%T", node)] = true
		return true
	})

	for _, want := range []string{
		"*ast.Block", "*ast.LocalStmt", "*ast.LocalFunction", "*ast.Function",
		"*ast.Vararg", "*ast.Do", "*ast.While", "*ast.Break", "*ast.Repeat",
		"*ast.If", "*ast.Return", "*ast.Binary", "*ast.Unary", "*ast.GenericFor",
		"*ast.Call", "*ast.Name", "*ast.Table", "*ast.FunctionStmt",
		"*ast.MethodCall", "*ast.Index", "*ast.String", "*ast.Goto", "*ast.Label",
		"*ast.Assign", "*ast.CallStmt", "*ast.Number", "*ast.Paren",
	} {
		assert.True(t, types[want], want)
	}
}

func TestInspectTableFields(t *testing.T) {
	t.Parallel()

	pos := ast.Pos{Line: 1, Col: 1}
	table := &ast.Table{At: pos, Fields: []*ast.Field{
		{Key: &ast.Number{At: pos, Raw: "1"}, Value: &ast.Number{At: pos, Raw: "2"}},
		{Value: &ast.Number{At: pos, Raw: "3"}},
		{Key: &ast.String{At: pos, Value: "f"}, Value: &ast.Number{At: pos, Raw: "4"}},
	}}

	var visited []string

	ast.Inspect(table, func(node ast.Node) bool {
		visited = append(visited, fmt.Sprintf("%T", node))
		return true
	})

	// A field without a key is visited through its value only.
	assert.Equal(t, []string{
		"*ast.Table",
		"*ast.Number", "*ast.Number",
		"*ast.Number",
		"*ast.String", "*ast.Number",
	}, visited)
}

func TestPosOfAllNodes(t *testing.T) {
	t.Parallel()

	pos := ast.Pos{Line: 2, Col: 3}

	nodes := []ast.Node{
		&ast.Nil{At: pos},
		&ast.True{At: pos},
		&ast.False{At: pos},
		&ast.Vararg{At: pos},
		&ast.Number{At: pos},
		&ast.String{At: pos},
		&ast.Name{At: pos},
		&ast.Index{At: pos},
		&ast.Call{At: pos},
		&ast.MethodCall{At: pos},
		&ast.Function{At: pos},
		&ast.Table{At: pos},
		&ast.Binary{At: pos},
		&ast.Unary{At: pos},
		&ast.Paren{At: pos},
		&ast.LocalStmt{At: pos},
		&ast.LocalFunction{At: pos},
		&ast.Assign{At: pos},
		&ast.CallStmt{At: pos},
		&ast.Do{At: pos},
		&ast.While{At: pos},
		&ast.Repeat{At: pos},
		&ast.If{At: pos},
		&ast.NumericFor{At: pos},
		&ast.GenericFor{At: pos},
		&ast.FunctionStmt{At: pos},
		&ast.Return{At: pos},
		&ast.Break{At: pos},
		&ast.Goto{At: pos},
		&ast.Label{At: pos},
		&ast.Block{Start: pos},
	}

	var exprs, stmts int

	for _, node := range nodes {
		if _, ok := node.(ast.Expr); ok {
			exprs++
		}

		if _, ok := node.(ast.Stmt); ok {
			stmts++
		}
	}

	// The unexported methods make the node an Expr or a Stmt: 15 of each;
	// the block is neither.
	assert.Equal(t, 15, exprs, "expressions")
	assert.Equal(t, 15, stmts, "statements")

	for _, node := range nodes {
		assert.Equal(t, pos, node.Pos(), "%T", node)
	}
}
