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
`import.meta` its properties.

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

89.8% of [test262](https://github.com/tc39/test262) pass: 43,823 of 48,797 tests
in `test/language`, `test/built-ins` and `test/annexB` (commit `2e58f87`). The tests that are
known to fail are listed in `testdata/test262-failures.txt`, and the test suite fails when
that list would grow.

Implemented:

- Nearly all of the syntax up to ES2025, including direct and indirect `eval` and the `Function`
  constructor
- Nearly all of the ES2025 standard library and of Annex B, with `RegExp` on its own
  backtracking engine
- Newer additions: explicit resource management, `Array.fromAsync`, `Error.isError`,
  `Math.sumPrecise`, the `Uint8Array` base64 and hex methods, immutable `ArrayBuffer`s, the `Map`
  upsert methods, `JSON.parse` source text access, the newer `Iterator` and `Promise` helpers,
  and the `stack` accessor of `Error.prototype`

Notes on behavior:

- Promise jobs run when the outermost call from Go into the VM returns. Unlike QuickJS, where the
  host drains them with `JS_ExecutePendingJob`, there is no way to run them later
- Promise rejections that nothing handled by then are reported to `VM.SetUnhandledRejection`,
  the way HTML fires `unhandledrejection`
- Typed array elements are stored in little-endian byte order on every platform
- `Date` uses the local time zone of Go's `time` package, or of the JavaScript host with `GOOS=js`
- `VM.SetTimeZone` and `VM.SetRandomSource` replace the time zone of `Date` and the source of
  `Math.random`, for deterministic runs
- Without `Intl`, the `toLocale*String` methods of `Date` use a fixed English format, and
  `localeCompare` treats canonically equivalent strings as equal and otherwise compares code units
- A VM is an agent that cannot block, so `Atomics.wait` throws. A finite `Atomics.waitAsync`
  timeout takes effect when the jobs run after it expired. The engine supports agents that share
  the memory of a `SharedArrayBuffer` across goroutines, with blocking waits, but no public API
  creates them yet
- Weak collections, `WeakRef` and `FinalizationRegistry` use Go's garbage collector
- Modules include dynamic `import()`, `import.meta`, JSON modules and `import defer`. Module
  loading is synchronous. Source phase imports parse, but no module has a source object

Missing:

- A public API for agents (workers): the engine can share a `SharedArrayBuffer` between VMs on
  different goroutines, but a host cannot create such VMs or hand them buffers yet
- `Intl` and `Temporal`

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
