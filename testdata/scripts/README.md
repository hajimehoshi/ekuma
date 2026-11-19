# Local JavaScript tests

These engine regression tests use Test262 front matter and an original 0BSD assertion harness.
They complement the external conformance suite, including implementation-specific
behavior and extensions. The Go runner reads the scripts and local harness
from `testdata` using `os.DirFS`. They run with `go test -short ./...` without a
Test262 checkout.

Put each test in a descriptive `.js` file grouped by feature. Use `assert.sameValue` for assertions and `negative` metadata for an uncaught
expected error. The local harness implements only `assert.sameValue`,
`Test262Error`, and `$DONE`.
Use `flags: [noStrict]` for sloppy scripts and `flags: [onlyStrict]` for strict
scripts. With neither flag, the shared runner runs both modes. Use `flags: [module]` for a
test that runs as a module, which is strict code. Async tests add `async` to the flags
(for example `flags: [noStrict, async]`) and complete with `$DONE`.

For example, run the local proxy tests with:

```sh
go test . -run '^TestScripts/proxy/'
```

Keep tests of Go APIs, host configuration, Go constants, script completion
values, and exact source positions in Go. Module loader integration tests also
remain in Go.
