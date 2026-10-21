package parser_test

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
	"github.com/bigbes/go-luascope/parser"
)

func ExampleParseBytes() {
	chunk, err := parser.ParseBytes([]byte("local n = 1LL\nprint(n, x)"), "@example.lua")
	if err != nil {
		panic(err)
	}

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		if name, ok := node.(*ast.Name); ok {
			fmt.Println(name.Name, name.At, "local:", name.Local != nil)
		}

		return true
	})

	// "@" marks a file name, like in LuaJIT.
	_, err = parser.ParseBytes([]byte("x = 1 +"), "@example.lua")
	fmt.Println(err)

	// Output:
	// print 2:1 local: false
	// n 2:7 local: true
	// x 2:10 local: false
	// example.lua:1: unexpected symbol near '<eof>'
}
