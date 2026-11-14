# Ekuma

Ekuma is a JavaScript engine written in pure Go: no cgo and no `unsafe`.
It is an interpreter without a JIT, designed to be fast for one.
Its only dependency is [`golang.org/x/text`](https://pkg.go.dev/golang.org/x/text),
for Unicode normalization.

The project is at an early stage. The API is unstable and a large part of the language's
standard library is missing. See [Status](#status).

```go
vm := ekuma.NewVM()
v, err := vm.RunScript(ctx, "hello.js", `"Hello, " + ["wor", "ld"].join("")`)
if err != nil {
	// A *ekuma.SyntaxError, or a *ekuma.Error when the script throws.
}
fmt.Println(v.String()) // Hello, world
```

The API follows [`syscall/js`](https://pkg.go.dev/syscall/js) where it can:
`Value.Get`, `Value.Set`, `Value.Call`, `Value.Invoke`, `Value.New`, `VM.ValueOf`, `VM.FuncOf`,
`CopyBytesToGo`, `CopyBytesToJS`.
Unlike `syscall/js`, a JavaScript exception is returned as an error, and the methods that run
a script or call a function take a `context.Context` that stops them.

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

About four fifths of [test262](https://github.com/tc39/test262) pass: 39,387 of 48,797 tests
in `test/language`, `test/built-ins` and `test/annexB` (commit `2e58f87`). The tests that are
known to fail are listed in `testdata/test262-failures.txt`, and the test suite fails when
that list would grow.

Implemented:

- Most of the syntax up to ES2025: classes with private members and static blocks, generators,
  destructuring, spread, template literals, optional chaining, `let` and `const` with
  the temporal dead zone, getters and setters, labels, `with`
- `Object`, `Function`, `Array`, `String`, `Number`, `Boolean`, `Symbol`, `Math`, `Reflect`,
  the error types, and array, string and generator iterators, including `Math.sumPrecise`,
  `Error.isError` and the `stack` accessor of `Error.prototype`
- `Iterator` with `Iterator.from`, the iterator helpers such as `map` and `flatMap`,
  `Iterator.concat`, `Iterator.zip`, `Iterator.zipKeyed`, `chunks`, `windows`, `includes`
  and `join`
- `RegExp`, with its own backtracking engine: all flags including `v`, named groups, lookbehind,
  Unicode property escapes, pattern modifiers, `RegExp.escape` and the legacy static properties
- `JSON`, including `JSON.rawJSON`, `JSON.isRawJSON` and the `context` argument of a reviver
- `Map` and `Set` with their iterators, the Set methods such as `union`, `getOrInsert`,
  `getOrInsertComputed`, `Map.groupBy` and `Object.groupBy`
- `WeakMap`, `WeakSet`, `WeakRef` and `FinalizationRegistry`, on Go's garbage collector:
  an entry of a weak collection does not keep its key alive
- `Date` and its Annex B methods, with the local time zone of Go's `time` package, or of the
  JavaScript host with `GOOS=js`; the `toLocale*String` methods use a fixed English format,
  since there is no `Intl`
- The locale-sensitive `String` methods use the root locale: `localeCompare` treats canonically
  equivalent strings as equal and otherwise compares code units
- `BigInt`, with BigInt literals and arithmetic
- `Promise`, including `Promise.any`, `Promise.withResolvers` and `Promise.try`; async functions,
  async generators, `await` and `for await`; `Array.fromAsync`. The jobs run when the outermost
  call from Go into the VM returns
- `ArrayBuffer` (including resizable buffers, `transfer`, and immutable buffers with
  `transferToImmutable` and `sliceToImmutable`), the typed arrays including
  `Float16Array` and the `Uint8Array` base64 and hex methods, and `DataView`; elements are
  stored in little-endian byte order on every platform
- `SharedArrayBuffer` and `Atomics` for a single agent: `Atomics.wait` throws, as on an agent
  that cannot block, and `Atomics.notify` wakes the waiters of `Atomics.waitAsync`. A finite
  `waitAsync` timeout takes effect when the jobs run after it expired

Missing:

- Direct `eval` and the `Function` constructor
- Modules and dynamic `import`
- `Proxy`
- Agents (workers) sharing a `SharedArrayBuffer`
- `Intl` and `Temporal`

Known inaccuracies in what is implemented: `arguments` is not linked to the parameters in
sloppy mode, parameter default values share the scope of the function body, and the `caller`
and `arguments` properties of sloppy functions are always `undefined`.

## Running the tests

```sh
go test ./...
```

The test262 run is skipped unless a checkout of the suite is found in `ekuma/test262`
under the user cache directory, or in the directory that `EKUMA_TEST262` names.

## License

[0BSD](LICENSE)
