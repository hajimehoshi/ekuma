# Ekuma

Ekuma is a JavaScript engine written in pure Go: no cgo and no `unsafe`.
It is an interpreter without a JIT, designed to be fast for one.
Its only dependency is [`golang.org/x/text`](https://pkg.go.dev/golang.org/x/text),
for Unicode normalization.

The project is at an early stage. The API is unstable and a large part of the language's
standard library is missing. See [Status](#status).

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

## Host API

The API follows [`syscall/js`](https://pkg.go.dev/syscall/js) where it can:
`Value.Get`, `Value.Set`, `Value.Call`, `Value.Invoke`, `Value.New`, `VM.ValueOf`, `VM.FuncOf`,
`CopyBytesToGo`, `CopyBytesToJS`.
Unlike `syscall/js`, a JavaScript exception is returned as an error, and the methods that run
a script or call a function take a `context.Context` that stops them.

### Modules

A module is parsed with `VM.ParseModule`, `VM.ParseJSONModule` or `VM.NewSyntheticModule`, and
the host supplies the modules that it imports with `VM.SetModuleLoader`, which `import()` uses
too. `Module.Evaluate` loads, links and evaluates a module and returns the promise of the
evaluation, whose state `Value.PromiseState` reads. `Module.LoadRequestedModules`,
`Module.Link` and `Module.Namespace` take the earlier steps alone, and `VM.SetImportMeta` gives
`import.meta` its properties. Module loading is synchronous.

### Host environment

Promise jobs run when the outermost call from Go into the VM returns, and the rejections that
are still unhandled then are reported to `VM.SetUnhandledRejection`. `Date` and `Temporal` use
the local time zone of Go's `time` package, or of the JavaScript host with `GOOS=js`, unless
`VM.SetTimeZone` sets one. `VM.SetRandomSource` sets the source of `Math.random`.

### Agents

VMs on different goroutines share the memory of a `SharedArrayBuffer` as agents do:
`Value.SharedDataBlock` returns the memory, which `VM.ValueOf` turns into a new
`SharedArrayBuffer` of another VM. `Atomics.wait` throws unless `VM.SetCanBlock` lets the VM
block. When another VM notifies a waiter of `Atomics.waitAsync`, its promise is settled by the
jobs that the waiting VM runs at the end of its current or next outermost call from Go.
`VM.AwaitJobs` blocks until such a waiter is notified or times out, and runs the jobs.

## Differences from other engines for Go

| | Ekuma | [goja](https://github.com/dop251/goja) | [modernc.org/quickjs](https://pkg.go.dev/modernc.org/quickjs) |
|---|---|---|---|
| Implementation | Written in Go | Written in Go | QuickJS, translated from C to Go |
| cgo | No | No | No |
| `unsafe` | No | Yes | Yes |
| Maturity | Early | Mature | Mature (QuickJS itself) |

What Ekuma does differently is aimed at speed:

- Objects have hidden-class shapes, and every property access site has an inline cache.
- The bytecode is register based, with fused compare-and-jump and constant-operand instructions.
- Calls between JavaScript functions allocate nothing and do not recurse in Go.
- A value is two words, and numbers are never allocated.
- Forwarding calls such as `f.apply(this, arguments)` do not create an arguments object.
- Memory is managed by Go's garbage collector, without reference counting.

Strings are Go strings in [WTF-8](https://simonsapin.github.io/wtf-8/), with exact UTF-16 semantics.
A well-formed string crosses the Go boundary without conversion.

Performance has only been measured on one synthetic workload so far
(`go test -bench Workload`, in `testdata/workload.js`). No comparison with other engines
is published yet.

## Status

100% of [test262](https://github.com/tc39/test262) pass: 48,829 of 48,829 tests
in `test/language`, `test/built-ins`, `test/annexB`, `test/intl402/BigInt`,
`test/intl402/Date`, `test/intl402/FallbackSymbol` and `test/intl402/Number` (commit
`2e58f87`). The test suite
fails when any of them fails.

### Implemented

- Nearly all of the syntax and standard library up to ES2025 and Annex B, with `RegExp` on its own
  backtracking engine
- `Temporal`, explicit resource management, `import defer`, and the newer `Array`, `Error`,
  `Math`, `Map`, `JSON`, `Iterator`, `Promise`, `Uint8Array` and `ArrayBuffer` additions
- `ShadowRealm`, which is a proposal and not yet standard
- Decorators, auto-accessors (`accessor`) and decorator metadata (`Symbol.metadata`), which are
  proposals and not yet standard

### Notes on behavior

- `Temporal` has only the `iso8601` calendar, and its named time zones need the system's time
  zone database
- Without `Intl`, `toLocale*String` uses a fixed English format, and `localeCompare` compares
  code units after canonical equivalence
- Source phase imports and `%AbstractModuleSource%` exist, but importing the source of any
  module throws a SyntaxError, since no host API gives a module a module source
- `ShadowRealm.prototype.importValue` loads modules with the loader of `VM.SetModuleLoader`,
  as `import()` in code without a file does. The loader cannot tell the realms apart, so a
  module that it returns again is shared by them
- `Function.prototype[Symbol.metadata]` is `null`, as the spec text of the decorator metadata
  proposal defines it, so a class without decorators inherits `null` unless its superclass has
  metadata
- The `caller` and `arguments` properties of sloppy functions, a legacy extension that the
  specification leaves to implementations, are always `undefined`. Browsers give the calling
  function and the arguments of the running call

### Missing

- Most of `Intl`. The optional package `github.com/hajimehoshi/ekuma/intl` adds it when it is
  imported for its side effect, and has `Intl.getCanonicalLocales`, `Intl.supportedValuesOf`,
  `Intl.Locale`, `Intl.NumberFormat`, `Intl.PluralRules` and `Intl.DateTimeFormat` so far.
  `Intl.DateTimeFormat` formats only the Gregorian calendar yet
- Host API for these, some of which the engine has and only the tests use:
  - Creating further realms in a VM
  - Giving a module a module source
  - Property keys other than strings: symbol keys, property descriptors, and listing the keys
    of an object
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
under the user cache directory, or in the directory that `EKUMA_TEST262` names.

## License

[0BSD](LICENSE), except for the data of the [Unicode Common Locale Data Repository](https://cldr.unicode.org/)
in `internal/intl/cldr`, which is under the [Unicode License v3](internal/intl/cldr/LICENSE).
Only the optional package `github.com/hajimehoshi/ekuma/intl` includes that data. A program that
imports it must include the copyright and permission notice of the Unicode License v3 with its
copies or in their documentation.
