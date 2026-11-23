# Ekuma

Ekuma is a JavaScript engine written in pure Go. Its bytecode interpreter runs without a JIT,
and it uses `unsafe` only for OS APIs. It implements nearly all of ES2026. The Go API is unstable.
See [Status](#status).

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

Compared on 2026-11-16, with goja and Sobek at their latest commits.

Ekuma uses a register-based interpreter, hidden classes, inline property caches, and dense
array storage. JavaScript calls reuse interpreter stack space, including proper tail calls.
On 64-bit platforms, a value occupies two words and can hold a number, a signed 64-bit BigInt,
or a short string inline. Strings use [WTF-8](https://simonsapin.github.io/wtf-8/), preserving
UTF-16 semantics and allowing well-formed strings to cross the Go boundary without transcoding.

## Performance

Time relative to Ekuma, as the geometric mean over the benchmarks of each suite (lower is
faster):

<!-- BEGIN jsbench summary -->

```
                     V8 benchmark  QuickJS
                     suite v7      microbench    Version
Ekuma                 1.00         1.00          v0.0.0-20261122162400-5d3411ee3ba0
goja                  7.35         5.81          v0.0.0-20261118204651-8dfece3d1e1d
Sobek                 7.19         5.83          v0.0.0-20261119094010-6eceb967bb5b
otto                 37.0 (9/10)    n/a          v0.5.1
modernc.org/quickjs   4.88         3.98          v0.27.0
QJS                   6.41 (6/10)  6.34 (69/74)  v0.0.6
QuickJS (C)           1.50         1.07          v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.821        0.721         v0.44.0
v8go                  0.0425       0.133         v0.44.0
```

Measured on 2026-11-23: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

`(k/n)` means that the engine and Ekuma both completed k of n benchmarks. The mean covers
those paired results. See the [benchmark method](_bench/README.md#method),
[individual results](_bench/RESULTS.md), and [Ekuma CPU profile](_bench/TUNING.md#cpu-profiling).

## Host API

The API follows [`syscall/js`](https://pkg.go.dev/syscall/js) for working with values and
functions. JavaScript exceptions return to Go as errors. Calls that execute scripts or invoke
functions take a `context.Context` for cancellation. The [package documentation](https://pkg.go.dev/github.com/hajimehoshi/ekuma)
describes cancellation, host panics, and pending jobs.

### Scripts

A parsed `Script` can be evaluated repeatedly. To share one parse across VMs, use
`ParseUnboundScript` and bind the result in each VM with `VM.BindScript`.

### Modules

Use `VM.ParseModule`, `VM.ParseJSONModule`, or `VM.NewSyntheticModule` to create a module.
The host supplies a synchronous loader through `VM.SetModuleLoader`, including for `import()`.
`Module.Evaluate` loads, links, and evaluates the module and returns a promise.

### Realms

`VM.NewRealm` creates a global object with its own built-ins. Realms in a VM share values,
jobs, and settings. Use separate VMs to isolate code.

### Promises and jobs

`VM.NewPromise` returns a promise and a resolver for completing it from Go. Pending promise
jobs run before the outermost Go call that executes JavaScript returns. `VM.SetUnhandledRejection`
receives rejections still unhandled after those jobs run. Promise continuations, including
`await`, preserve the host value set by `VM.WithContinuationData` when they were registered.

### Time and randomness

`Date`, `Temporal`, and `Intl.DateTimeFormat` use the local time zone by default. Override it
with `VM.SetTimeZone`, the clock with `VM.SetClock`, and the source of `Math.random` with
`VM.SetRandomSource`.

### Intl

Import the optional `intl` package to enable `Intl`, locale-aware string and formatting
methods, and additional `Temporal` calendars in every VM.

```go
import _ "github.com/hajimehoshi/ekuma/intl"
```

With Go 1.27.4 and default build flags, the import increases the hello-world binary
from 9.1 to 18.0 MiB on macOS arm64 and from 12.0 to 21.2 MiB on `js/wasm`.
`VM.SetLocale` overrides the host default locale. iOS builds with `intl` require `CGO_ENABLED=1`.
See the [intl package documentation](https://pkg.go.dev/github.com/hajimehoshi/ekuma/intl)
for host locale selection and [License](#license) for distribution notices.

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
4 fail for known reasons, which [`test262-failures.txt`](internal/jstest/testdata/test262-failures.txt) lists.

Ekuma checks its pure Go implementation of `Intl` (ECMA-402) against ICU’s own tests for
collation, segmentation, and locale-aware formatting. The suite also checks Unicode normalization
and `Temporal` calendars. Across the pinned ICU 78.3 and 79.1 fixtures, all 129 subtests passed
with 289 expected differences and zero unexpected failures. See the [ICU coverage inventory](internal/icutest/upstream-inventory.md).

### Implemented

- Nearly all of the syntax and standard library up to ES2026 and Annex B, with `RegExp` on its own
  backtracking engine
- `Temporal`, explicit resource management, `import defer`, and the newer `Error`, `Iterator`,
  `Promise` and `ArrayBuffer` additions
- `ShadowRealm`, which is a proposal and not yet standard
- Decorators, auto-accessors (`accessor`) and decorator metadata (`Symbol.metadata`), which are
  proposals and not yet standard

### Notes on behavior

- `Date` truncates historical UTC offsets to whole minutes. `Temporal.ZonedDateTime` uses
  the full offset for a named time zone.
- Ekuma uses the Unicode version shipped with Go (17.0.0 in Go 1.27). Identifiers containing
  letters added in later Unicode versions may be rejected.
- Native builds load named time zones from system zoneinfo and Go's `time.LoadLocation` sources.
  With `GOOS=js`, the host's `Intl.DateTimeFormat` must also recognize the zone. Local `Date`
  operations use the JavaScript host's `Date` by default.
- Without [Intl](#intl), `Temporal` supports only the `iso8601` calendar, `toLocale*String`
  uses a fixed English format, `localeCompare` compares normalized code units, and case
  conversion ignores the locale.
- `Intl.Collator` follows the CLDR root collation, in which Han characters sort by code point,
  and the tailorings of CLDR; ICU sorts Han by radical and stroke in the root
- `Intl.Segmenter` uses dictionary word boundaries for Chinese, Japanese, Thai, Khmer and
  Burmese text in every locale. Greek locales also tailor sentence breaks at question marks
- `Intl.DisplayNames` uses a regular CLDR variant's canonical subtag when the display
  locale has no translation. If another subtag has no display name, the result is the
  canonical language code, or `undefined` with `fallback: "none"`
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

Use `go test -short ./...` to run the [local regression tests](testdata/scripts) and skip
external suites. Add `-tags=ekumavalidate` to check bytecode generation and execution.

### Conformance tests

`go test .` runs the external suites when their checkouts are available. Set the environment
variables below or place the checkouts at the listed paths under the user cache directory.
The [CI workflow](.github/workflows/test.yml) lists the sparse checkout paths.

| Suite | Environment variable | Default cache path | CI revision | Expectations |
|---|---|---|---|---|
| [ICU 78.3 (Unicode 17, CLDR 48.2 collation)](https://github.com/unicode-org/icu/tree/release-78.3) | `EKUMA_ICU` | `ekuma/icu` | [Revision](internal/icutest/testdata/icu-revision.txt) | [Expected failures](internal/icutest/testdata/icu-failures.txt) |
| [ICU 79.1 (CLDR 49 locale and formatting data)](https://github.com/unicode-org/icu/tree/release-79.1) | `EKUMA_ICU_CLDR` | `ekuma/icu-cldr` | [Revision](internal/icutest/testdata/icu-cldr-revision.txt) | [Expected failures](internal/icutest/testdata/icu-failures.txt) |
| [Test262](https://github.com/tc39/test262) | `EKUMA_TEST262` | `ekuma/test262` | [Revision](internal/jstest/testdata/test262-revision.txt) | [Expected failures](internal/jstest/testdata/test262-failures.txt) |
| V8 [Intl](https://github.com/v8/v8/tree/main/test/intl), [mjsunit](https://github.com/v8/v8/tree/main/test/mjsunit), [Mozilla](https://github.com/v8/v8/tree/main/test/mozilla), and [WebKit](https://github.com/v8/v8/tree/main/test/webkit) | `EKUMA_V8` | `ekuma/v8` | [Revision](internal/jstest/testdata/v8-revision.txt) | [Test expectations](internal/jstest/testdata/v8-failures.txt) |

See the [ICU fixture inventory](internal/icutest/README.md) for coverage and focused test commands.

For V8's Mozilla suite, fetch the data revision pinned by V8 from the Ekuma repository root:

```sh
bash internal/jstest/checkout-mozilla.sh "$EKUMA_V8"
```

With both V8 and Test262 checked out, `TestV8Test262` also runs V8's local Test262 cases.

Use `-test262.dir` or `-v8.dir` to select a checkout-relative directory, or the corresponding
`filter` flag to match part of a test path.

```sh
go test -count=1 -v -run '^TestTest262$' . -test262.dir test/intl402/Collator
go test -count=1 -v -run '^TestV8$' . -v8.dir test/intl/collator
```

## License

Ekuma's code is licensed under [0BSD](LICENSE). The data tables Ekuma generates
from Unicode, CLDR, and ICU releases are licensed under the [Unicode License v3](internal/unicode/LICENSE).
The historical [Chinese](internal/intl/calendar/NOTICE-lunarsolar) and [Korean](internal/intl/calendar/NOTICE-dotnet) calendar tables are licensed under the MIT License.

Every build includes Unicode character and emoji data, so all distributions must
include the Unicode copyright and permission notice.

- Windows builds must also include the [CLDR time zone mapping notice](internal/datetime/NOTICE),
  even when built without `intl`.
- Builds that import `github.com/hajimehoshi/ekuma/intl` must also include the following notices.
  - [CLDR locale data](internal/unicode/cldr/NOTICE)
  - [CLDR collation data](internal/unicode/collation/NOTICE)
  - [ICU calendar data](internal/intl/calendar/NOTICE)
  - [Projected Chinese calendar data from lunar-go (MIT)](internal/intl/calendar/NOTICE-lunar-go)
  - [Historical Chinese calendar data from Lunar-Solar-Calendar-Converter (MIT)](internal/intl/calendar/NOTICE-lunarsolar)
  - [Historical Korean calendar data from .NET (MIT)](internal/intl/calendar/NOTICE-dotnet)
  - ICU4C word break dictionaries
    - [Thai](internal/unicode/wordbreak/NOTICE-thai)
    - [Khmer](internal/unicode/wordbreak/NOTICE-khmer)
    - [Burmese](internal/unicode/wordbreak/NOTICE-burmese)
    - [Chinese/Japanese](internal/unicode/wordbreak/NOTICE-cj)
