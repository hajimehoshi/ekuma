# Local JavaScript tests

These regression tests use Test262 front matter and the [local 0BSD harness](../../internal/jstest/testdata/harness/).
Run them with `go test -short ./...` without a Test262 checkout.

Group `.js` files by feature. Use `assert.sameValue` for assertions and `negative` metadata
for expected uncaught errors. Cross-realm tests can include `realm-assertions.js`.

Scripts run in both sloppy and strict mode by default. Set `noStrict`, `onlyStrict`, or
`module` in `flags` to select a mode. Async tests add `async` and finish with `$DONE`.

Without `-short`, each script also runs compiled without the compiler's assignment shortcuts, and
three times in one VM. Both runs must show what the normal run shows: the completion, the values
of the assertions, the printed lines, and the stack frames of an uncaught error. A script with
top-level lexical declarations is put in a block to repeat it. A script that cannot run twice in
a realm sets `noRepeat` in `flags` and gives the reason in `noRepeatReason`. Module tests run
only normally.

For example, run the local proxy tests with:

```sh
go test . -run '^TestScripts$/^proxy$/'
```

Keep tests of Go APIs, host configuration, Go constants, script completion
values, and exact source positions in Go. Module loader integration tests also
remain in Go.
