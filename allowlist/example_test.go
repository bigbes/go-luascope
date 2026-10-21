package allowlist_test

import (
	"fmt"

	"github.com/bigbes/go-luascope/allowlist"
	"github.com/bigbes/go-luascope/parser"
)

func ExampleChecker() {
	chunk, err := parser.ParseBytes([]byte(`
local fiber = require('fiber')
box.cfg{}
box.space.users:replace({1})
counter = 1
`), "migration.lua")
	if err != nil {
		panic(err)
	}

	checker, err := allowlist.NewChecker(allowlist.Rules{
		Modules: []string{"log", "json"},
		Globals: []string{"require", "pairs", "box.space.*"},
	})
	if err != nil {
		panic(err)
	}

	for _, v := range checker.Check(chunk) {
		fmt.Println(v)
	}

	// Output:
	// 2:15: module 'fiber' is not allowed
	// 3:1: global variable 'box.cfg' is not allowed
	// 5:1: assignment to global variable 'counter'
}
