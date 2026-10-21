# go-luascope

[![CI](https://github.com/bigbes/go-luascope/actions/workflows/ci.yml/badge.svg)](https://github.com/bigbes/go-luascope/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/bigbes/go-luascope.svg)](https://pkg.go.dev/github.com/bigbes/go-luascope)
[![Go Report Card](https://goreportcard.com/badge/github.com/bigbes/go-luascope)](https://goreportcard.com/report/github.com/bigbes/go-luascope)

Static analysis of Lua code written for [Tarantool](https://www.tarantool.io).

Lua code that runs inside Tarantool can reach the whole server: the `box`
API, the fibers, the modules installed on the instance. A tool that runs
untrusted code, checks a migration before applying it, or reviews a stored
procedure needs to know which globals and which modules that code touches,
and needs a good answer for code that does not even compile. `go-luascope`
answers both: it parses Lua the way Tarantool's LuaJIT does, with the checks
of the LuaJIT compiler, and it resolves every name to a local variable, an
upvalue or a global. On top of that, it reports which global variables and
modules the code uses and checks them against allow-lists.

| Package | Purpose |
|:---|:---|
| `parser` | Parses a chunk into an AST, with the checks of the LuaJIT compiler. |
| `ast` | The syntax tree and `Inspect`. |
| `globals` | Global variables, their fields, and `require` calls used by a chunk. |
| `allowlist` | Checks the globals and modules of a chunk against allow-lists. |

## Quick start

```sh
go get github.com/bigbes/go-luascope
```

```go
package main

import (
	"fmt"
	"os"

	"github.com/bigbes/go-luascope/allowlist"
	"github.com/bigbes/go-luascope/globals"
	"github.com/bigbes/go-luascope/parser"
)

func main() {
	f, err := os.Open("migration.lua")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer f.Close()

	chunk, err := parser.Parse(f, "migration.lua")
	if err != nil {
		// A LuaJIT-like message: "migration.lua:3: unexpected symbol near 'x'".
		fmt.Println(err)
		return
	}

	for _, require := range globals.Analyze(chunk).Requires {
		fmt.Println(require.Pos, require.Module) // 1:14 json
	}

	checker, err := allowlist.NewChecker(allowlist.Rules{
		Modules: []string{"crud", "json"},
		Globals: []string{"require", "string.*", "box.space.*"},
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, v := range checker.Check(chunk) {
		fmt.Println(v) // Nothing: the chunk uses only allowed names.
	}
}
```

## Dialect

The lexer and the parser are ports of `lj_lex.c`, `lj_strscan.c` and
`lj_parse.c` of [tarantool/luajit](https://github.com/tarantool/luajit),
built the way Tarantool builds it: with FFI and without
`LUAJIT_ENABLE_LUA52COMPAT`. Besides Lua 5.1, this means:

- `goto` and `::labels::` with LuaJIT's scoping rules; `goto` is a keyword
  only before a name, so `local goto = 1` is valid;
- `\xXX`, `\z` and `\u{XXX}` escapes;
- binary literals `0b101`, hexadecimal fractions and exponents `0x1.8p1`;
- `1i`, `1LL`, `1ULL` and `1LLU` number suffixes;
- any byte 128-255 in identifiers, a UTF-8 BOM and a `#!` line;
- a `break` must be the last statement of a block, there are no empty
  statements (`;;`), and a call cannot start on a new line
  (`ambiguous syntax`), as in Lua 5.1.

`parser.Parse` reads a chunk from any `io.Reader`, `parser.ParseBytes` takes
the source in memory. A failure to read the reader is reported as a
`parser.ReadError`, never as a syntax error of the chunk.

The parser does the compiler's checks: gotos and labels, `break` outside a
loop, `...` outside a vararg function, assignment targets, and the limits on
local variables, upvalues and syntax nesting. Error messages, lines and the
tokens they are reported near are the same as LuaJIT's.

Differences from LuaJIT:

- registers are not allocated, so a function that LuaJIT rejects as
  `function or expression too complex` is accepted;
- bytecode chunks are always rejected; they cannot be analyzed.

## Globals and allow-lists

`globals.Analyze` reports one access per use of a global name, with the path
of constant fields after it: `box.space.users:select()` is a read of
`box.space.users.select`, `box.space[name]` is a read of `box.space[?]`,
`t.x = 1` is a write of `t.x` when `t` is global. The accesses match the
`GGET` and `GSET` instructions LuaJIT emits for the chunk.

Calls of the global `require` are reported with the module name when it is a
string constant or a concatenation of string constants: `require("log")`,
`pcall(require, "log")` and `xpcall(require, handler, "log")`. Other uses of
`require`, like `local r = require`, are reported separately, because the
module they load cannot be known.

An entry of `allowlist.Rules` allows exactly the module or the global path it
names. A `*` element matches any one name, including a field indexed with a
non-constant key, and a `*` at the end matches one or more names:

| Entry | Allows | Does not allow |
|:---|:---|:---|
| `string` | `local s = string` | `string.format()` |
| `string.*` | `string.format()` | `local s = string` |
| `box.space.*` | `box.space.users:select()`, `box.space[name]` | `pairs(box.space)` |
| `box.space.*.select` | `box.space.users:select()`, `box.space[name]:select()` | `box.space.users:replace()` |
| `crud` | `require("crud")` | `require("crud.common")` |
| `crud.*` | `require("crud.common.utils")` | `require("crud")` |

Writing global variables is never allowed. A `require` with a computed module
name and `require` used as a value are violations.

The analysis is static and is not a sandbox: code can still reach globals
through allowed functions, like `rawget` or `getfenv`, if the lists allow
them.

## Documentation

The reference is on [pkg.go.dev](https://pkg.go.dev/github.com/bigbes/go-luascope),
including runnable examples for every package. Locally:

```sh
go doc ./allowlist  # the package documentation of any package
pkgsite -http=:6060  # or: go run golang.org/x/pkgsite/cmd/pkgsite@latest
```

`go doc` and `pkgsite` render the same comments that
[pkg.go.dev](https://pkg.go.dev/github.com/bigbes/go-luascope) shows.

## Requirements

- Go 1.24 or later, no C toolchain and no Tarantool installation: the
  library is pure Go.
- The only dependency is `github.com/stretchr/testify`, used by the tests.
- Linux, macOS and Windows are supported; CI runs the tests on Linux.

## Testing

The parser is compared with LuaJIT on:

- the Lua 5.1 test suite, the LuaJIT, lua-Harness and Tarantool tests of
  tarantool/luajit, and the Lua sources of Tarantool, vshard and crud, see
  [`parser/testdata/corpus`](parser/testdata/corpus/SOURCES.md);
- 4 random edits of every corpus file;
- 2000 generated programs that mix scopes, closures, loops and gotos;
- hand-written snippets for each lexer and parser rule.

For each source the result must match: either the same error, or the same
global reads and writes as the `GGET` and `GSET` instructions of the
compiled chunk. The reference results are stored in
`parser/testdata/golden`, so the tests do not need LuaJIT. To regenerate
them, build the LuaJIT of Tarantool and run:

```sh
git clone --depth 1 https://github.com/tarantool/luajit /tmp/luajit
rm -rf /tmp/luajit/.git  # A shallow clone has no tags for the version.
cmake -S /tmp/luajit -B /tmp/luajit-build -DLUAJIT_ENABLE_GC64=ON
make -C /tmp/luajit-build -j
make golden LUAJIT=/tmp/luajit-build/src/luajit
```

`make test` runs the tests, `make testrace` runs them a hundred times with
the race detector, `make cover` prints the statement coverage, `make lint`
runs golangci-lint, `make spell` runs codespell, and `make fuzz` runs the
parser fuzzer.

The statement coverage of the library is above 98%, and every package has
tests.

## Contributing

Issues and pull requests are welcome, see [CONTRIBUTING.md](CONTRIBUTING.md).
Changes are listed in [CHANGELOG.md](CHANGELOG.md).

## License

BSD 2-Clause, see [LICENSE](LICENSE). The test corpora keep their own
licenses, see [SOURCES.md](parser/testdata/corpus/SOURCES.md).
