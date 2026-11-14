# Ekuma

Ekuma is a JavaScript engine written in pure Go: no cgo and no `unsafe`.
It is an interpreter without a JIT, designed to be fast for one.

The project is at an early stage. The API is unstable and a large part of the language's
standard library is missing. See [Status](#status).

```go
vm := ekuma.New()
v, err := vm.RunScript(ctx, "hello.js", `"Hello, " + ["wor", "ld"].join("")`)
if err != nil {
	// A *ekuma.SyntaxError, or a *ekuma.Error when the script throws.
}
fmt.Println(v.String()) // Hello, world
```

The API follows [`syscall/js`](https://pkg.go.dev/syscall/js) where it can:
`Value.Get`, `Value.Set`, `Value.Call`, `Value.Invoke`, `Value.New`, `VM.ValueOf`, `VM.FuncOf`.
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

More than half of [test262](https://github.com/tc39/test262) passes: 26,728 of 48,797 tests
in `test/language`, `test/built-ins` and `test/annexB` (commit `2e58f87`). The tests that are
known to fail are listed in `testdata/test262-failures.txt`, and the test suite fails when
that list would grow.

Implemented:

- Most of the syntax up to ES2025: classes with private members and static blocks, generators,
  destructuring, spread, template literals, optional chaining, `let` and `const` with
  the temporal dead zone, getters and setters, labels, `with`
- `Object`, `Function`, `Array`, `String`, `Number`, `Boolean`, `Symbol`, `Math`, the error types,
  and array, string and generator iterators
- `RegExp`, with its own backtracking engine: all flags including `v`, named groups, lookbehind,
  Unicode property escapes, pattern modifiers, `RegExp.escape` and the legacy static properties
- BigInt literals and arithmetic (but not the `BigInt` object)

Missing:

- `async` functions, `for await` and `Promise`
- Direct `eval` and the `Function` constructor
- Modules and dynamic `import`
- `Map`, `Set`, `WeakMap`, `WeakSet`, `Date`, `JSON`, `Proxy`, `Reflect`, the `BigInt` object
- Typed arrays, `ArrayBuffer`, `DataView`
- `Intl` and `Temporal`

Known inaccuracies in what is implemented: `arguments` is not linked to the parameters in
sloppy mode, and parameter default values share the scope of the function body.

## Running the tests

```sh
go test ./...
```

The test262 run is skipped unless a checkout of the suite is found in `ekuma/test262`
under the user cache directory, or in the directory that `EKUMA_TEST262` names.

## License

[0BSD](LICENSE)
