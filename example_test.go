package luascope_test

import (
	"fmt"

	"github.com/bigbes/go-luascope/globals"
	"github.com/bigbes/go-luascope/parser"
)

func Example() {
	chunk, err := parser.ParseBytes([]byte("box.space.users:select()"), "example.lua")
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, access := range globals.Analyze(chunk).Accesses {
		fmt.Println(access.Pos, access.Path, access.Write)
	}

	// Output:
	// 1:1 box.space.users.select false
}
