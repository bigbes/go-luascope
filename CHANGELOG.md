# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- `parser`: the chunk name passed to `Parse` and `ParseBytes` follows the
  convention of LuaJIT, and error messages format it the way LuaJIT does:
  `"@file.lua"` is printed as `file.lua`, with the start cut off if it is
  long; `"=name"` is printed as `name`; any other name is taken for the
  source of the chunk and printed as `[string "name"]`. Pass `"@" + path`
  for a file to keep messages like `file.lua:3: ...`; a name passed as it
  was before is now printed as `[string "file.lua"]:3: ...`. A `ReadError`
  formats the name the same way. The errors of bytecode chunks name the
  chunk like the bytecode loader of LuaJIT, and the error for bytecode after
  a header does not name it, like LuaJIT. `SyntaxError.Chunk` and
  `ReadError.Chunk` keep the name as passed.
- `parser`: parsing takes about 20% more time, because the parser emits
  the bytecode of LuaJIT to reject the code LuaJIT cannot encode, see
  below. On the 6.2 MB of Lua of the test corpus, `BenchmarkParseCorpus`
  went from 76.4 to 62.9 MB/s, and from 17.4 to 21.8 ms on the files under
  5 KB. Parsers reuse their memory through a `sync.Pool`, and the names,
  numbers and comments of the AST are substrings of one copy of the source,
  which they keep alive: a parse allocates 23% fewer objects and 4% more
  bytes.

### Fixed

- `globals`: a string constant in parentheses used as a key is a constant
  field, as it is for LuaJIT: `box[("schema")].space.create()` is a read of
  `box.schema.space.create`, not of `box[?].space.create`.
- `globals`: a chain of fields is analyzed once, so the time and memory of
  `Analyze` are linear in its length. A chain rooted at a local variable or
  a call, like `local t = {} return t.x.x.x`, was analyzed again from each
  of its fields, which was quadratic: 10000 fields took 1.5 seconds.
- `parser`: the limit of LuaJIT on the variable stack is checked. A chunk
  with more than 65476 locals, gotos and labels declared in a function and
  the functions enclosing it, active or not, is rejected with LuaJIT's
  error "chunk has more than 65476 local variables"; it was accepted.
- `parser`: the code that the code generator of LuaJIT cannot encode is
  rejected with the errors of LuaJIT; it was accepted. That is a jump over
  more than 32767 instructions ("control structure too long"), a function
  that needs more than 250 registers ("function or expression too
  complex"), a return too far from the end of its function to be fixed up
  ("function too long for return fixup"), more than 65536 constants in a
  function, more than 2^26 instructions in the functions being compiled.

## [0.1.0] - 2026-10-21

The first public release.

### Added

- `parser`: parses a chunk of Lua code in the dialect of Tarantool's LuaJIT,
  performs the checks of the LuaJIT compiler, and resolves every name to a
  local variable, an upvalue, or a global variable. `Parse` reads the chunk
  from an `io.Reader` and `ParseBytes` takes it in memory; a failure to read
  the reader is reported as a `ReadError` with the chunk name.
- `ast`: the syntax tree of a chunk and `Inspect` to walk it.
- `globals`: reports the global variables, with the paths of their constant
  fields, and the `require` calls a chunk uses, matching the `GGET` and
  `GSET` instructions of the compiled chunk.
- `allowlist`: checks the globals and modules of a chunk against lists of
  allowed names, with wildcards for fields and submodules.

[Unreleased]: https://github.com/bigbes/go-luascope/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/bigbes/go-luascope/releases/tag/v0.1.0
