package parser_test

import (
	"fmt"

	"github.com/bigbes/go-luascope/ast"
	"github.com/bigbes/go-luascope/parser"
)

func ExampleParse() {
	chunk, err := parser.Parse([]byte("local n = 1LL\nprint(n, x)"), "example")
	if err != nil {
		panic(err)
	}

	ast.Inspect(chunk.Block, func(node ast.Node) bool {
		if name, ok := node.(*ast.Name); ok {
			fmt.Println(name.Name, name.At, "local:", name.Local != nil)
		}

		return true
	})

	_, err = parser.Parse([]byte("x = 1 +"), "example")
	fmt.Println(err)

	// Output:
	// print 2:1 local: false
	// n 2:7 local: true
	// x 2:10 local: false
	// example:1: unexpected symbol near '<eof>'
}
