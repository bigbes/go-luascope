Please check before you request a review:

- [ ] The change is complete: no leftover debug output, no unrelated edits.
- [ ] Every behavior change has a test; a bug fix has a test that fails
      without the fix.
- [ ] New functionality is documented: a package doc comment or an
      `ExampleXxx` function.
- [ ] `CHANGELOG.md` describes the change for a user of the library.
- [ ] `make test`, `make lint` and `make spell` pass locally.
- [ ] If the dialect or the checks have changed, the comparison with LuaJIT
      is still green; if reference results were regenerated, say so below.

What changes for a user of the library:

<!-- Describe the change the way CHANGELOG.md does. -->

Related issues:

<!-- Closes #1234 -->
