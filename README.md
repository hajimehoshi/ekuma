# Ekuma

Ekuma is a JavaScript engine written in pure Go: no cgo and no `unsafe`.
It is an interpreter without a JIT, designed to be fast for one.
Its only dependency is [`golang.org/x/text`](https://pkg.go.dev/golang.org/x/text),
for Unicode normalization.

The project is at an early stage. The API is unstable and a large part of the language's
standard library is missing. See [Status](#status).

```go
var vm ekuma.VM
v, err := vm.RunScript(ctx, "hello.js", `"Hello, " + ["wor", "ld"].join("")`)
if err != nil {
	// A *ekuma.SyntaxError, or a *ekuma.Error when the script throws.
}
fmt.Println(v.String()) // Hello, world
```

The zero value of `ekuma.VM` is ready to use. A `VM` must not be copied after first use.

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
`VM.SetTimeZone` sets one. `VM.SetRandomSource` sets the source of `Math.random`. There is no
public API yet for agents (workers) that share a `SharedArrayBuffer`, or for creating further
realms in a VM.

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

99.7% of [test262](https://github.com/tc39/test262) pass: 48,637 of 48,797 tests
in `test/language`, `test/built-ins` and `test/annexB` (commit `2e58f87`). The tests that are
known to fail are listed in `testdata/test262-failures.txt`, and the test suite fails when
that list would grow.

### Implemented

- Nearly all of the syntax and standard library up to ES2025 and Annex B, with `RegExp` on its own
  backtracking engine
- `Temporal`, explicit resource management, `import defer`, and the newer `Array`, `Error`,
  `Math`, `Map`, `JSON`, `Iterator`, `Promise`, `Uint8Array` and `ArrayBuffer` additions

### Notes on behavior

- `Temporal` has only the `iso8601` calendar, and its named time zones need the system's time
  zone database
- Without `Intl`, `toLocale*String` uses a fixed English format, and `localeCompare` compares
  code units after canonical equivalence
- `Atomics.wait` throws, as a VM cannot block
- Source phase imports have no source objects

### Missing

- `Intl`

Known inaccuracies in what is implemented: the `caller` and `arguments` properties of sloppy
functions are always `undefined`.

## Running the tests

```sh
go test ./...
```

The test262 run is skipped unless a checkout of the suite is found in `ekuma/test262`
under the user cache directory, or in the directory that `EKUMA_TEST262` names.

## License

[0BSD](LICENSE)
