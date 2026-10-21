# Contributing

Issues and pull requests are welcome.

## Reporting a bug

Open an issue with the **Bug report** template and include:

- the Go version and the operating system;
- the smallest Lua chunk that shows the problem, inline or as a file;
- what you expected and what you got: an error message, a position, an AST
  node, a reported global;
- for a difference from LuaJIT, the version of Tarantool's LuaJIT and the
  message it prints.

## Proposing a feature

Open an issue with the **Feature request** template and describe the problem
before the solution: which Lua code you cannot analyze today and what you
need to know about it.

## Pull requests

Before you request a review, check the change yourself:

- the change is complete: no leftover debug output, no unrelated edits;
- the diff is explainable line by line and stays within 500-700 lines,
  excluding removed files;
- tests pass locally: `make test`, `make lint`, `make spell`;
- every behavior change has a test, and a bug fix has a test that fails
  without the fix;
- new functionality is documented: a package doc comment, `doc.go`, or an
  `ExampleXxx` function, which is compiled and executed by the tests;
- `CHANGELOG.md` describes the change for a user of the library.

Keep the parser honest: a change of the dialect or of the checks must keep
the comparison with LuaJIT green. If the change affects the reference
results, regenerate them with `make golden` and say so in the pull request.

## Commit messages

Write in English, one logical change per commit:

```text
prefix: commit title

The commit body explains what changed and why, not how. Lines are up to
72 characters wide.

Closes #1234
```

- The title is a short prefix, a colon, a space, and a topic in the
  imperative mood with a lowercase first letter: `parser: accept goto in
  nested scopes`, `doc: describe the dialect`.
- Keep the title within about 50 characters and without a trailing period.
- Reference issues in the body, in its last lines: `Part of #1234` for an
  intermediate commit, `Closes #1234` in the final commit of a series.
- Use your real name and email: `git config user.name` and
  `git config user.email`.

Common prefixes are `parser`, `ast`, `globals`, `allowlist`, `tests`, `doc`
and `ci`.

## Style

- The code follows the [Google Go style guide][style] and
  [CodeReviewComments][comments].
- Comments, identifiers and documentation are in English.
- Exported identifiers have doc comments that end with a period and start
  with the name being documented.
- `golangci-lint` runs in CI with the configuration from `.golangci.yml`;
  run `make lint` before pushing.
- Use the standard library unless a dependency earns its place.

[style]: https://google.github.io/styleguide/go/guide
[comments]: https://go.dev/wiki/CodeReviewComments

## Branches

`master` is the main branch. Develop in `<username>/<topic>` and open a pull
request against `master`.

## Review

A pull request is merged after the maintainer approves it and CI is green.
The review is in English. The reviewer resolves the threads they opened.
