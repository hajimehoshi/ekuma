# Ekuma

Ekuma is a JavaScript engine written in pure Go and is almost `unsafe`-free.
It uses `unsafe` only to access OS APIs.
It is designed for fast execution without a JIT compiler.

The project is at an early stage and its API is unstable, though nearly all of the syntax and
standard library up to ES2026 is implemented. See [Status](#status).

```go
var vm ekuma.VM
s, err := vm.ParseScript("hello.js", `"Hello, " + ["wor", "ld"].join("")`)
if err != nil {
	// A *ekuma.SyntaxError.
}
v, err := s.Evaluate(ctx)
if err != nil {
	// A *ekuma.ScriptError when the script throws.
}
fmt.Println(v.String()) // Hello, world
```

The zero value of `ekuma.VM` is ready to use. A `VM` must not be copied after first use.

## Differences from other engines for Go

| | Ekuma | [goja](https://github.com/dop251/goja) | [Sobek](https://github.com/grafana/sobek) | [otto](https://github.com/robertkrimen/otto) | [modernc.org/quickjs](https://pkg.go.dev/modernc.org/quickjs) | [QJS](https://github.com/fastschema/qjs) | [v8go](https://github.com/tommie/v8go) |
|---|---|---|---|---|---|---|---|
| Latest release | None | None | None | v0.5.1 (2024-11-05) | v0.27.0 (2026-10-24) | v0.0.6 (2025-10-28) | v0.44.0 (2026-11-12) |
| Implementation | Written in Go | Written in Go | Written in Go | Written in Go | QuickJS, translated from C to Go | QuickJS-ng, compiled to WebAssembly and run by wazero | Bindings to V8 |
| Execution strategy | Bytecode interpreter | Bytecode interpreter | Bytecode interpreter | AST interpreter | Bytecode interpreter | Bytecode interpreter, compiled to machine code by wazero's AOT compiler on amd64 and arm64 | JIT compiler, or bytecode interpreter with `--jitless` |
| Pure Go | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ |
| `unsafe`-free | ⚠️ Except for OS API access | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| ECMAScript | ES2026 | ES5.1, and much of ES2015 and later | ES5.1, and much of ES2015 and later | ES5 | ES2023 | ES2023 | ES2025 |
| Modules | ✅ | ❌ | ⚠️ Experimental | ❌ | ✅ | ✅ | ❌ |
| `Intl` | ✅ | ❌ | ❌ | ❌ | ⚠️ `DateTimeFormat` and `NumberFormat` in en-US | ❌ | ✅ |
| `Temporal` | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

- Investigated on 2026-11-16, with goja and Sobek at their latest commits.
- Sobek is the fork of goja that k6 uses.
- v8go is the fork of [rogchap.com/v8go](https://github.com/rogchap/v8go) that follows V8's
  releases.

Ekuma uses the following techniques to improve performance:

- Objects have hidden-class shapes, and named property accesses use inline caches.
- Dense array elements use contiguous storage with direct indexed access.
- The bytecode is register based, with fused compare-and-jump and constant-operand instructions.
- Ordinary JavaScript calls reuse stack storage within the interpreter loop, without Go recursion.
- Proper tail calls reuse the current frame for eligible strict-mode calls.
- Values occupy two words on 64-bit platforms; numbers and signed 64-bit BigInts are stored inline.
  Short primitive strings can also store up to eight WTF-8 bytes inline, avoiding a separate
  heap object.
- Eligible forwarding calls such as `f.apply(this, arguments)` avoid creating an arguments object.
- Memory is managed by Go's garbage collector, without reference counting.
- Strings use [WTF-8](https://simonsapin.github.io/wtf-8/) with exact UTF-16 semantics;
  well-formed strings cross the Go boundary without transcoding.
- Repeated string appends and prepends can reuse growing buffers, preserving earlier string values.
  Known ASCII operands can extend those buffers directly.
- Multi-part string concatenation sizes its output before copying and can assemble small results
  in stack scratch space.

## Performance

Time relative to Ekuma, as the geometric mean over the benchmarks of each suite (lower is
faster):

<!-- BEGIN jsbench summary -->

```
                     V8 benchmark  QuickJS
                     suite v7      microbench    Version
Ekuma                 1.00         1.00          v0.0.0-20261120154757-f130b4d6c37d
goja                  6.89         4.58          v0.0.0-20261118204651-8dfece3d1e1d
Sobek                 6.94         4.59          v0.0.0-20261119094010-6eceb967bb5b
otto                 34.9 (9/10)    n/a          v0.5.1
modernc.org/quickjs   4.77         3.14          v0.27.0
QJS                   6.24 (6/10)  5.00 (69/74)  v0.0.6
QuickJS (C)           1.46         0.841         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.814        0.569         v0.44.0
v8go                  0.0411       0.108         v0.44.0
```

Measured on 2026-11-21: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

- For reference, the results include QuickJS compiled from the C source used by
  modernc.org/quickjs, and v8go with and without V8's JIT compilers.
- "(k/n)" means that k of the n benchmarks had successful paired results for that engine and
  Ekuma; the mean covers those benchmarks. otto cannot parse microbench.js, which uses ES2015
  syntax. Failed rounds are excluded from timing ratios.
- See the [benchmark method](_bench/README.md#method), [individual results](_bench/RESULTS.md),
  and [Ekuma CPU profile](_bench/TUNING.md#cpu-profiling).

## Host API

The API follows [`syscall/js`](https://pkg.go.dev/syscall/js) where it can:
`Value.Get`, `Value.Set`, `Value.Call`, `Value.Invoke`, `Value.New`, `VM.ValueOf`, `VM.FuncOf`,
`CopyBytesToGo`, `CopyBytesToJS`.
Unlike `syscall/js`, a JavaScript exception is returned as an error, and the methods that run
a script or call a function take a `context.Context` that can cancel execution.
`Value.GetWithValueKey`, `Value.SetWithValueKey`, `Value.DeleteWithValueKey` and
`Value.CallWithValueKey` take the property key as a value, such as a symbol.
`VM.NewArrayBuffer` makes an `ArrayBuffer` over a Go byte slice without copying, which only
`VM.DetachArrayBuffer` detaches, as WebAssembly memory needs when it grows.

### Scripts

`VM.ParseScript` parses a script for a VM, and `Script.Evaluate` evaluates it, as often as
needed. A host that runs the same script in many VMs can parse it once with
`ParseUnboundScript` and give each VM its own `Script` with `VM.BindScript`, which does not
parse it again. An `UnboundScript` is immutable, and VMs on different goroutines may bind it
at the same time.

### Modules

Use `VM.ParseModule` or `VM.ParseJSONModule` to parse a module, or `VM.NewSyntheticModule` to
create a synthetic module. The host supplies imported modules through `VM.SetModuleLoader`,
which also handles `import()`. Module loading is synchronous.

`Module.Evaluate` loads, links and evaluates a module and returns a promise whose state can
be read with `Value.PromiseState`. `Module.LoadRequestedModules`
and `Module.Link` perform the loading and linking steps separately.

`Module.Namespace` returns the module namespace, and `VM.SetImportMeta` sets the properties
of `import.meta`.

### Realms

`VM.NewRealm` creates a realm: a global object with its own built-in objects, in which
`Realm.ParseScript`, `Realm.BindScript`, `Realm.ParseModule`, `Realm.ValueOf` and
`Realm.FuncOf` work. The methods of `VM` work in `VM.InitialRealm`. `ModuleRequest.Realm`
tells the module loader the realm of an import, including that of a `ShadowRealm`.

The realms of a VM share its jobs and settings, and values pass between them, so a realm
is not a security boundary: an object of a realm gives access to its built-in objects.
A separate `VM` isolates code.

### Promises

`VM.NewPromise` returns a pending promise and a `PromiseResolver`, which the host keeps to
settle the promise later, for example when a goroutine posts the result of I/O back to the
goroutine that uses the VM. `PromiseResolver.Resolve` and `PromiseResolver.Reject` settle it as
the resolving functions of a JavaScript promise do, and run the pending jobs like other calls
from Go. `Value.PromiseState` reads the state of any promise.

### Jobs

Pending promise jobs run before the outermost Go call that executes JavaScript returns,
including property operations and calls that throw. Calls reentered from host callbacks leave
jobs to the outermost call. An interruption discards pending jobs. Rejections still unhandled
after draining are reported to `VM.SetUnhandledRejection`.

`VM.WithContinuationData` calls a function with a host value as the continuation data, which
`VM.ContinuationData` returns. A promise job, including the continuation of an `await`, runs
with the continuation data that was current when it was registered, so the host can carry
request-scoped state such as an AsyncLocalStorage frame across asynchronous code.

### Cancellation

A context stops everything that runs under it. When a `Func` makes a call with another
context and that context stops the call, the outermost call returns the cause of that context
and the `Func` does not continue. A context that is done after its call returned stops
nothing. Operations without a context can run JavaScript and jobs without a cancellation
limit, unless reentered under a call with a context.

### Panics

A panic of a `Func` or of another host function passes through to the caller of the
Go call unchanged and leaves the VM usable. A host callback may also recover a panic from
its reentrant call and continue.

After a panic, pending jobs and unhandled rejection reports are discarded at the outermost
boundary, and unfinished module evaluations are rejected, even if
an inner panic was recovered. Synchronous module evaluations abandoned by a panic are
rejected before control returns to the host. Side effects are not rolled back.

Aborted generators are closed without running JavaScript `catch` or `finally` blocks.
Promises for abandoned async functions, async generator requests, and discarded jobs may
remain pending; later requests
on an aborted async generator see it as completed. Suspended generators outside the aborted
call remain intact.

Host callbacks must not call `runtime.Goexit`: it is not a panic and leaves the VM unusable.

### Time and randomness

`Date` and `Temporal` use
the local time zone of Go's `time` package, or of the JavaScript host with `GOOS=js`, unless
`VM.SetTimeZone` sets one. `VM.SetClock` sets the clock of the current time, and
`VM.SetRandomSource` the source of `Math.random`.

### Intl

Importing `github.com/hajimehoshi/ekuma/intl` for its side effects adds `Intl` to every VM.
It enables locale-aware formatting for the `toLocale*String` methods of numbers, dates,
durations, arrays and typed arrays, and locale-aware behavior for `localeCompare`,
`toLocaleUpperCase` and `toLocaleLowerCase`. It also adds the ECMA-402 calendars to `Temporal`,
in addition to `iso8601`:

```go
import _ "github.com/hajimehoshi/ekuma/intl"
```

Without the import, these features do not contribute to binary size. With it, a hello-world
program grows from about 9.1 MiB to 17.2 MiB on macOS arm64, and from 12.0 MiB to 20.4 MiB
with `GOOS=js`.

Set the VM default locale with `vm.SetLocale("ja-JP")`; `vm.SetLocale("")` restores
the host default. Explicit JavaScript locale arguments take precedence, and existing
Intl instances keep their locale.

The host default comes from `LC_ALL`, `LC_MESSAGES` or `LANG`, in that order, then
the OS locale: user preferences on macOS, iOS, and Windows, system properties on
Android, or system locale configuration on Linux.
FreeBSD, OpenBSD, NetBSD, and DragonFly BSD use the locale environment variables
supplied by the login session or shell. Browser builds use `navigator.language`.
Unavailable host locales, `C` and `POSIX` fall back to en-US.

The native locale calls on Apple platforms and Android use purego. Android builds
with `CGO_ENABLED=0` use only the locale environment variables. iOS builds require
`CGO_ENABLED=1`; macOS supports `CGO_ENABLED=0`.

Programs using the package must include the notices for its locale data and word dictionaries.
See [License](#license).

### Agents

VMs on different goroutines share the memory of a `SharedArrayBuffer` as agents do:
`Value.SharedDataBlock` returns the memory, which `VM.ValueOf` turns into a new
`SharedArrayBuffer` of another VM.

`Atomics.wait` throws unless `VM.SetCanBlock` lets the VM block. When another VM notifies
a waiter of `Atomics.waitAsync`, its promise is settled by the
jobs that the waiting VM runs at the end of its current or next outermost call from Go.
`VM.AwaitJobs` blocks until such a waiter is notified or times out, and runs the jobs.

## Status

53,791 of the 53,795 tests of [test262](https://github.com/tc39/test262) pass (99.99%). The other
4 fail for known reasons, which [`test262-skips.txt`](internal/jstest/testdata/test262-skips.txt) lists.

### Implemented

- Nearly all of the syntax and standard library up to ES2026 and Annex B, with `RegExp` on its own
  backtracking engine
- `Temporal`, explicit resource management, `import defer`, and the newer `Error`, `Iterator`,
  `Promise` and `ArrayBuffer` additions
- `ShadowRealm`, which is a proposal and not yet standard
- Decorators, auto-accessors (`accessor`) and decorator metadata (`Symbol.metadata`), which are
  proposals and not yet standard

### Notes on behavior

- `Date` truncates UTC offsets to whole minutes when calculating local time. At
  `1890-01-01T00:00:00Z` in `Europe/Paris`, `Date` uses `+00:09` for the historical
  offset of `+00:09:21`, so `getSeconds()` returns `0`. `Temporal.ZonedDateTime` uses
  the full offset supplied by the platform for a named time zone, giving `21` seconds here
- Without [Intl](#intl), `Temporal` supports only the `iso8601` calendar
- Native builds can load named time zones from system zoneinfo directories and from the sources
  available to Go's `time.LoadLocation`. These include a directory or uncompressed zip file named
  by `ZONEINFO`, Go's `$GOROOT/lib/time/zoneinfo.zip`, and embedded data from `time/tzdata` when
  imported. With `GOOS=js`, a named zone is available when the host's `Intl.DateTimeFormat`
  resolves its identifier and Go's `time.LoadLocation` can load the rules. Go supplies both the
  offsets and the transitions, with `time/tzdata` embedded for hosts without zoneinfo files.
  Local `Date` operations use the JavaScript host's `Date` by default
- Without [Intl](#intl), `toLocale*String` uses a fixed English format, `localeCompare`
  compares code units after Unicode normalization, and `toLocaleUpperCase` and
  `toLocaleLowerCase` ignore the locale. With Intl, these methods use locale-aware behavior
- `Intl.Collator` follows the CLDR root collation, in which Han characters sort by code point,
  and the tailorings of CLDR; ICU sorts Han by radical and stroke in the root
- `Intl.Segmenter` uses dictionary word boundaries for Thai and Han text in every locale.
  It follows the default UAX #29 rules for other scripts, including Japanese kana
- `Intl.DisplayNames` gives the code of a language, or `undefined` with `fallback: "none"`, when
  one of its subtags has no name, where ICU names the other subtags
- `Intl.DurationFormat` separates the fields of the `digital` style as CLDR's duration patterns
  do, such as `1.02.03` in Serbian, where ICU uses the time separator
- A BigInt has at most 2^30 bits, as in V8: an operation or a conversion whose result would be
  larger throws a RangeError, and a larger literal is a SyntaxError. `ValueOf` panics for a
  larger `*big.Int`, and a `Func` that returns one throws a RangeError
- Source phase imports and `%AbstractModuleSource%` exist, but importing the source of any
  module throws a SyntaxError, since no host API gives a module a module source
- `Function.prototype[Symbol.metadata]` is `null`, as the spec text of the decorator metadata
  proposal defines it, so a class without decorators inherits `null` unless its superclass has
  metadata
- The `caller` and `arguments` properties of sloppy functions, a legacy extension that the
  specification leaves to implementations, are accessors of each function. Browsers instead
  change the accessors of `Function.prototype`, which the specification defines to throw. As in
  SpiderMonkey, `caller` is `null` for a call from strict code, a generator, an async function or
  a built-in function. A proper tail call from strict code leaves no frame of the strict
  function, so the function that it calls sees the caller of the strict function instead

### Missing

Host API for these, some of which the engine has and only the tests use:

- Giving a module a module source
- Property descriptors, and listing the keys of an object
- Limits on stack depth or memory, and statistics of the memory that a VM uses

## Running the tests

```sh
go test ./...
```

Run `go test -tags=ekumavalidate ./...` to check the bytecode the compiler emits and verify
that the interpreter's register writes and control flow agree with the opcode definitions.

The [local JavaScript regression tests](testdata/scripts) use the
Test262 runner with an original 0BSD harness. They run with `go test -short ./...`
without a Test262 checkout.

### Conformance tests

`go test .` runs ICU, Test262, and V8 when their checkouts are available. Set the
suite's environment variable to its checkout directory, or use the default path
under the user cache directory. CI uses the pinned revisions below. For V8, a
sparse checkout needs `DEPS`, `test/intl`, `test/mjsunit`, `test/mozilla`, `test/test262/local-tests/test`,
`test/webkit`, the tool modules matching `/tools/**/*.mjs`, and the printing helper
`tools/clusterfuzz/js_fuzzer/resources/differential_fuzz_library.js`.
Short mode skips all external conformance tests.

| Suite | Environment variable | Default cache path | CI revision | Expected failures |
|---|---|---|---|---|
| [ICU](https://github.com/unicode-org/icu/tree/release-78.3) | `EKUMA_ICU` | `ekuma/icu` | [Revision](internal/icutest/testdata/icu-revision.txt) | None |
| [Test262](https://github.com/tc39/test262) | `EKUMA_TEST262` | `ekuma/test262` | [Revision](internal/jstest/testdata/test262-revision.txt) | [Skip list](internal/jstest/testdata/test262-skips.txt) |
| V8 [Intl](https://github.com/v8/v8/tree/main/test/intl), [mjsunit](https://github.com/v8/v8/tree/main/test/mjsunit), [Mozilla](https://github.com/v8/v8/tree/main/test/mozilla), and [WebKit](https://github.com/v8/v8/tree/main/test/webkit) | `EKUMA_V8` | `ekuma/v8` | [Revision](internal/jstest/testdata/v8-revision.txt) | [Skip list](internal/jstest/testdata/v8-skips.txt) |

`TestICU` uses Unicode 17.0 fixtures from ICU 78.3. It checks the UTF-16 boundaries
returned by `Intl.Segmenter` against the grapheme, word, and sentence break tests in
`icu4c/source/test/testdata`. It also checks `String.prototype.normalize` in NFC, NFD,
NFKC, and NFKD for all five columns of each row in
`icu4c/source/data/unidata/NormalizationTest.txt`. The fixtures retain their
[upstream license](https://github.com/unicode-org/icu/blob/release-78.3/LICENSE).
Run the suite on its own with

```sh
go test -count=1 -v -run '^TestICU$' .
```

After checking out V8, fetch the separate Mozilla data revision pinned in
its `DEPS` into `test/mozilla/data`. From the Ekuma repository root, run

```sh
bash internal/jstest/checkout-mozilla.sh "$EKUMA_V8"
```

`TestV8Test262` runs V8's local Test262 cases when both checkouts are available.
It reads the cases from V8 and the harness from Test262. Each case is a Go subtest
named by its path under `test/test262/local-tests/test`. The official Test262
totals count only files from the Test262 checkout.

```sh
go test -count=1 -v -run '^TestV8Test262$' .
```

`TestTest262` and `TestV8` use the same flags, prefixed with `-test262.` or
`-v8.`. Use `dir` to select a checkout-relative directory and `filter` to match
a substring of a test path.

```sh
go test -count=1 -v -run '^TestTest262$' . -test262.dir test/intl402/Collator
go test -count=1 -v -run '^TestV8$' . -v8.dir test/intl/collator
go test -count=1 -v -run '^TestV8$' . -v8.dir test/mjsunit/es6
go test -count=1 -v -run '^TestV8$' . -v8.dir test/mozilla/data/ecma_3/RegExp
go test -count=1 -v -run '^TestV8$' . -v8.dir test/webkit
```

Use `shuffle` with `off`, `on`, or an integer seed. To replay a file order,
set `GOMAXPROCS=1` and reuse the seed. `report FILE` writes tab-separated test
paths and failure reasons. Both suites flag skip-list errors in the console.
V8 also includes them in the report file. Relative checkout and report paths are
resolved from the repository root.

Both skip lists accept checkout-relative file paths and directory paths. End a
directory path with `/` to match every test beneath it, including in
subdirectories. The most specific entry for a test takes precedence.

Plain entries in each skip list are expected failures. The tests still run, and
an unexpected pass or a stale path fails the suite. A runner error, engine
panic, or timeout on the final attempt also fails the suite. In the V8 list, a
`# SKIP: reason` comment skips all consecutive test paths directly below it.
A `# SKIP(64-bit): reason` comment applies only on targets with 64-bit Go integers.
A `# PASS/FAIL: reason` group runs each test and accepts either a pass or a
JavaScript failure. The summary counts the observed outcome.
A blank line or comment ends the group.
The V8 runner loads checkout-relative scripts named by `// Files:` in order.
Intl and mjsunit load them after the suite harness and before the test. WebKit
loads them first, then runs `resources/standalone-pre.js`, the test, and
`resources/standalone-post.js`. It compares stdout with the paired
`-expected.txt` file using V8's line comparison rules. Unsupported flags and
environment settings skip the test.

Mozilla runs the `ecma`, `ecma_2`, `ecma_3`, and `js1_1` through `js1_5`
directories with `--expose-gc`. It loads V8's shell emulation followed by
each ancestor's `shell.js` from the data root to the test directory. A
JavaScript failure or `FAILED!` anywhere in stdout satisfies a negative
test (`-n.js`) and fails a positive test.

## License

Ekuma's code is under [0BSD](LICENSE). Programs that import the optional
`github.com/hajimehoshi/ekuma/intl` package must include the
[notices for the CLDR data and ICU4C word dictionaries](internal/unicode/LICENSE)
with their distributions.
