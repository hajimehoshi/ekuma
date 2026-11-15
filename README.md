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

### Realms

`VM.NewRealm` creates a realm: a global object with its own built-in objects, in which
`Realm.ParseScript`, `Realm.ParseModule`, `Realm.ValueOf` and `Realm.FuncOf` work. The methods
of `VM` work in `VM.InitialRealm`. `ModuleRequest.Realm` tells the module loader the realm of
an import, including that of a `ShadowRealm`. The realms of a VM share its jobs and settings,
and values pass between them, so a realm is not a security boundary: an object of a realm
gives access to its built-in objects. A separate `VM` isolates code.

### Host environment

Promise jobs run when the outermost call from Go into the VM returns, and the rejections that
are still unhandled then are reported to `VM.SetUnhandledRejection`. `Date` and `Temporal` use
the local time zone of Go's `time` package, or of the JavaScript host with `GOOS=js`, unless
`VM.SetTimeZone` sets one. `VM.SetRandomSource` sets the source of `Math.random`.

### Intl

Importing the package `github.com/hajimehoshi/ekuma/intl` for its side effect adds `Intl` to
every VM, makes the `toLocale*String` methods of numbers, dates, durations, arrays and typed
arrays format by locale and `localeCompare`, `toLocaleUpperCase` and `toLocaleLowerCase` follow
it, and gives `Temporal` the calendars of ECMA-402 besides `iso8601`:

```go
import _ "github.com/hajimehoshi/ekuma/intl"
```

Without the import, the engine has none of these, and its binary does not grow. With it, a hello
world grows from about 8.6 MB to 15.7 MB natively, and from 11.0 MB to 18.6 MB with `GOOS=js`.
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

- Nearly all of the syntax and standard library up to ES2025 and Annex B, with `RegExp` on its own
  backtracking engine
- `Temporal`, explicit resource management, `import defer`, and the newer `Array`, `Error`,
  `Math`, `Map`, `JSON`, `Iterator`, `Promise`, `Uint8Array` and `ArrayBuffer` additions
- `ShadowRealm`, which is a proposal and not yet standard
- Decorators, auto-accessors (`accessor`) and decorator metadata (`Symbol.metadata`), which are
  proposals and not yet standard

### Notes on behavior

- `Temporal` has only the `iso8601` calendar without [Intl](#intl). Its named time zones need
  the system's time zone database
- Without [Intl](#intl), `toLocale*String` uses a fixed English format, `localeCompare`
  compares code units after canonical equivalence, and `toLocaleUpperCase` and
  `toLocaleLowerCase` ignore the locale. With it, all of these follow the locale
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

Ekuma is under [0BSD](LICENSE), except for the data of the
[Unicode Common Locale Data Repository](https://cldr.unicode.org/) in `internal/unicode/cldr`
and its collation data in `internal/unicode/collation`, which are under the Unicode License v3
([license](internal/unicode/cldr/LICENSE), [license](internal/unicode/collation/LICENSE)). Only
the optional package `github.com/hajimehoshi/ekuma/intl` includes this data, so only a program
that imports that package must include the copyright and permission notice of the Unicode
License v3 with its copies or in their documentation.
