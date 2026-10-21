# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic
Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- `globals`: a string constant in parentheses used as a key is a constant
  field, as it is for LuaJIT: `box[("schema")].space.create()` is a read of
  `box.schema.space.create`, not of `box[?].space.create`.

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
