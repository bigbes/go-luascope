// Package luascope statically analyzes Lua code written for Tarantool.
//
// Every use of a global variable or of a module is reported with its
// position, so the caller can allow exactly the handful of names it needs:
//
//	chunk, err := parser.Parse(src, "migration.lua")
//	if err != nil {
//		return err
//	}
//
//	for _, access := range globals.Analyze(chunk).Accesses {
//		log.Println(access.Pos, access.Path, access.Write)
//	}
//
// The parser accepts the dialect of Tarantool's LuaJIT and performs the
// checks of its compiler, see the parser package.
//
// Subpackages:
//
//   - [github.com/bigbes/go-luascope/parser] parses a chunk into a syntax tree.
//   - [github.com/bigbes/go-luascope/ast] declares the syntax tree and Inspect.
//   - [github.com/bigbes/go-luascope/globals] finds the globals and modules
//     that code uses.
//   - [github.com/bigbes/go-luascope/allowlist] checks them against lists of
//     allowed names.
package luascope
