# Local test harness

These original 0BSD helpers implement the subset needed by the local JavaScript
tests: `assert.sameValue`, `Test262Error`, and `$DONE`. They are not copies of the
Test262 harness and do not provide its full API.

The filenames and async completion messages match the shared Go runner's
interface. `assert.sameValue` treats NaN as equal to NaN and distinguishes signed
zero. `$DONE()` reports success; `$DONE(error)` reports failure when the argument
is not undefined.

The external Test262 suite uses the upstream harness from its own checkout.
