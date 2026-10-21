package globals_test

import (
	"fmt"

	"github.com/bigbes/go-luascope/globals"
	"github.com/bigbes/go-luascope/parser"
)

func ExampleAnalyze() {
	chunk, err := parser.ParseBytes([]byte(`
local log = require('log')
local name = 'users'
box.space[name]:replace({1})
counter = (counter or 0) + 1
`), "example")
	if err != nil {
		panic(err)
	}

	usage := globals.Analyze(chunk)
	for _, r := range usage.Requires {
		fmt.Println(r.Pos, "require", r.Module)
	}

	for _, a := range usage.Accesses {
		fmt.Println(a.Pos, a.Path, "write:", a.Write)
	}

	// Output:
	// 2:13 require log
	// 2:13 require write: false
	// 4:1 box.space[?].replace write: false
	// 5:1 counter write: true
	// 5:12 counter write: false
}
