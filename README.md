# Ekuma

Ekuma is a JavaScript engine written in pure Go, without `unsafe`.
It is designed for fast execution without a JIT compiler.
Its only dependency is [`golang.org/x/text`](https://pkg.go.dev/golang.org/x/text),
for Unicode normalization and case mapping.

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
	// A *ekuma.Error when the script throws.
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
| `unsafe`-free | ✅ | ❌ | ❌ | ✅ | ❌ | ❌ | ❌ |
| ECMAScript | ES2026 | ES5.1, and much of ES2015 and later | ES5.1, and much of ES2015 and later | ES5 | ES2023 | ES2023 | ES2025 |
| Modules | ✅ | ❌ | ⚠️ Experimental | ❌ | ✅ | ✅ | ❌ |
| `Intl` | ✅ | ❌ | ❌ | ❌ | ⚠️ `DateTimeFormat` and `NumberFormat` in en-US | ❌ | ✅ |
| `Temporal` | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

- Investigated on 2026-11-16, with goja and Sobek at their latest commits.
- Sobek is the fork of goja that k6 uses.
- v8go is the fork of [rogchap.com/v8go](https://github.com/rogchap/v8go) that follows V8's
  releases.

Ekuma uses the following techniques to improve performance:

- Objects have hidden-class shapes, and every property access site has an inline cache.
- The bytecode is register based, with fused compare-and-jump and constant-operand instructions.
- Calls between JavaScript functions allocate nothing and do not recurse in Go.
- A value is two words, and numbers and BigInts that fit in 64 bits are never allocated.
- Forwarding calls such as `f.apply(this, arguments)` do not create an arguments object.
- Memory is managed by Go's garbage collector, without reference counting.

Strings are Go strings in [WTF-8](https://simonsapin.github.io/wtf-8/), with exact UTF-16 semantics.
A well-formed string crosses the Go boundary without conversion.

## Performance

Time relative to Ekuma, as the geometric mean over the benchmarks of each suite (lower is
faster):

<!-- BEGIN jsbench summary -->

```
                     V8 benchmark  QuickJS
                     suite v7      microbench    Version
Ekuma                 1.00         1.00          v0.0.0-20261116080942-4f50ec950347
goja                  3.94         3.35          v0.0.0-20261110204830-f493a2b600d6
Sobek                 3.93         3.34          v0.0.0-20261105103427-a5b602015248
otto                 21.8 (9/10)    n/a          v0.5.1
modernc.org/quickjs   2.87         2.33          v0.27.0
QJS                   3.45 (6/10)  3.68 (69/74)  v0.0.6
QuickJS (C)           0.884        0.626         v0.0.0-20260604125329-3d5e064e9dd6
v8go --jitless        0.486        0.424         v0.44.0
v8go                  0.0252       0.0797        v0.44.0
```

Measured on 2026-11-16: Apple M3 Pro, macOS 27.2, go1.27.4.

<!-- END jsbench summary -->

- For reference, the results include QuickJS compiled from the C source used by
  modernc.org/quickjs, and v8go with and without V8's JIT compilers.
- "(k/n)" means that the engine ran k of the n benchmarks and failed the others, and its mean is
  over the ones it ran. otto cannot parse microbench.js, which uses ES2015 syntax.
- See [`_bench`](_bench/README.md) for the benchmarks, measurement methodology, and individual
  results.

## Host API

The API follows [`syscall/js`](https://pkg.go.dev/syscall/js) where it can:
`Value.Get`, `Value.Set`, `Value.Call`, `Value.Invoke`, `Value.New`, `VM.ValueOf`, `VM.FuncOf`,
`CopyBytesToGo`, `CopyBytesToJS`.
Unlike `syscall/js`, a JavaScript exception is returned as an error, and the methods that run
a script or call a function take a `context.Context` that can cancel execution.
`Value.GetWithValueKey`, `Value.SetWithValueKey`, `Value.DeleteWithValueKey` and
`Value.CallWithValueKey` take the property key as a value, such as a symbol.

### Modules

Use `VM.ParseModule` or `VM.ParseJSONModule` to parse a module, or `VM.NewSyntheticModule` to
create a synthetic module. The host supplies imported modules through `VM.SetModuleLoader`,
which also handles `import()`. `Module.Evaluate` loads, links and evaluates a module and returns
a promise whose state can be read with `Value.PromiseState`. `Module.LoadRequestedModules`
and `Module.Link` perform the loading and linking steps separately. `Module.Namespace` returns
the module namespace, and `VM.SetImportMeta` sets the properties of `import.meta`. Module loading
is synchronous.

### Realms

`VM.NewRealm` creates a realm: a global object with its own built-in objects, in which
`Realm.ParseScript`, `Realm.ParseModule`, `Realm.ValueOf` and `Realm.FuncOf` work. The methods
of `VM` work in `VM.InitialRealm`. `ModuleRequest.Realm` tells the module loader the realm of
an import, including that of a `ShadowRealm`. The realms of a VM share its jobs and settings,
and values pass between them, so a realm is not a security boundary: an object of a realm
gives access to its built-in objects. A separate `VM` isolates code.

### Host environment

Pending promise jobs run before the outermost Go call that executes JavaScript returns,
including property operations and calls that throw. Calls reentered from host callbacks leave
jobs to the outermost call. An interruption discards pending jobs. Rejections still unhandled
after draining are reported to `VM.SetUnhandledRejection`. Operations without a context can
run JavaScript and jobs without a cancellation limit, unless reentered under a call with a
context.

A context stops everything that runs under it. When a `Func` makes a call with another
context and that context stops the call, the outermost call returns the cause of that context
and the `Func` does not continue. A context that is done after its call returned stops
nothing. A panic of a `Func` or of another host function passes through to the caller of the
Go call unchanged and leaves the VM usable. A host callback may also recover a panic from
its reentrant call and continue. At the outermost boundary, pending jobs and unhandled
rejection reports are discarded, and unfinished module evaluations are rejected, even if
an inner panic was recovered. Synchronous module evaluations abandoned by a panic are
rejected before control returns to the host. Aborted generators are closed without running JavaScript
`catch` or `finally` blocks. Side effects are not rolled back. Promises for abandoned async
functions, async generator requests, and discarded jobs may remain pending; later requests
on an aborted async generator see it as completed. Suspended generators outside the aborted
call remain intact. Host callbacks must not call `runtime.Goexit`: it is not a panic and
leaves the VM unusable.

`Date` and `Temporal` use
the local time zone of Go's `time` package, or of the JavaScript host with `GOOS=js`, unless
`VM.SetTimeZone` sets one. `VM.SetRandomSource` sets the source of `Math.random`.

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
program grows from about 8.7 MB to 15.8 MB natively, and from 11.1 MB to 18.8 MB with `GOOS=js`.
The default locale is the one that `LC_ALL`, `LC_MESSAGES` or `LANG` names natively, or
`navigator.language` with `GOOS=js`, and en-US otherwise. The package brings locale data whose
license notice a program must include: see [License](#license).

### Agents

VMs on different goroutines share the memory of a `SharedArrayBuffer` as agents do:
`Value.SharedDataBlock` returns the memory, which `VM.ValueOf` turns into a new
`SharedArrayBuffer` of another VM. `Atomics.wait` throws unless `VM.SetCanBlock` lets the VM
block. When another VM notifies a waiter of `Atomics.waitAsync`, its promise is settled by the
jobs that the waiting VM runs at the end of its current or next outermost call from Go.
`VM.AwaitJobs` blocks until such a waiter is notified or times out, and runs the jobs.

## Status

53,789 of the 53,795 tests of [test262](https://github.com/tc39/test262) pass (99.99%). The other
6 fail for known reasons, which [`testdata/test262-skips.txt`](testdata/test262-skips.txt) lists.

### Implemented

- Nearly all of the syntax and standard library up to ES2026 and Annex B, with `RegExp` on its own
  backtracking engine
- `Temporal`, explicit resource management, `import defer`, and the newer `Error`, `Iterator`,
  `Promise` and `ArrayBuffer` additions
- `ShadowRealm`, which is a proposal and not yet standard
- Decorators, auto-accessors (`accessor`) and decorator metadata (`Symbol.metadata`), which are
  proposals and not yet standard

### Notes on behavior

- Without [Intl](#intl), `Temporal` supports only the `iso8601` calendar. Named time zones require
  the system's time zone database
- Without [Intl](#intl), `toLocale*String` uses a fixed English format, `localeCompare`
  compares code units after Unicode normalization, and `toLocaleUpperCase` and
  `toLocaleLowerCase` ignore the locale. With Intl, these methods use locale-aware behavior
- `Intl.Collator` follows the CLDR root collation, in which Han characters sort by code point,
  and the tailorings of CLDR; ICU sorts Han by radical and stroke in the root. `Intl.Segmenter`
  follows UAX #29 without dictionaries, so it does not split Chinese, Japanese or Thai text into
  words
- `Intl.DisplayNames` gives the code of a language, or `undefined` with `fallback: "none"`, when
  one of its subtags has no name, where ICU names the other subtags. `Intl.DurationFormat`
  separates the fields of the `digital` style as CLDR's duration patterns do, such as `1.02.03`
  in Serbian, where ICU uses the time separator
- Source phase imports and `%AbstractModuleSource%` exist, but importing the source of any
  module throws a SyntaxError, since no host API gives a module a module source
- `Function.prototype[Symbol.metadata]` is `null`, as the spec text of the decorator metadata
  proposal defines it, so a class without decorators inherits `null` unless its superclass has
  metadata
- The `caller` and `arguments` properties of sloppy functions, a legacy extension that the
  specification leaves to implementations, are accessors of each function. Browsers instead
  change the accessors of `Function.prototype`, which the specification defines to throw. As in
  SpiderMonkey, `caller` is `null` for a call from strict code, a generator, an async function or
  a built-in function

### Missing

- Host API for these, some of which the engine has and only the tests use:
  - Giving a module a module source
  - Property descriptors, and listing the keys of an object
  - Creating or awaiting a promise from Go other than through the `Promise` constructor and
    `then`
  - Sharing a Go byte slice with an `ArrayBuffer`, which `CopyBytesToGo` and `CopyBytesToJS`
    copy
  - Replacing the clock of `Date.now`, which `testing/synctest` fakes in Go tests
  - Limits on stack depth or memory, and statistics of the memory that a VM uses

## Running the tests

```sh
go test ./...
```

The test262 run is skipped unless a checkout of the suite is found in `ekuma/test262`
under the user cache directory, or in the directory specified by `EKUMA_TEST262`.

## License

Ekuma is under [0BSD](LICENSE), except for the data of the
[Unicode Common Locale Data Repository](https://cldr.unicode.org/) in `internal/unicode/cldr`
and its collation data in `internal/unicode/collation`, which are under the Unicode License v3
([license](internal/unicode/cldr/LICENSE), [license](internal/unicode/collation/LICENSE)). Only
the optional package `github.com/hajimehoshi/ekuma/intl` includes this data, so only a program
that imports that package must include the copyright and permission notice of the Unicode
License v3 with its copies or in their documentation.
