// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/hajimehoshi/ekuma"
)

// memoryModules loads the modules of sources by name, ignoring the referrer, and keeps one
// record per name. loads counts the calls of the loader for each name.
type memoryModules struct {
	vm      *ekuma.VM
	sources map[string]string
	modules map[string]*ekuma.Module
	loads   map[string]int
}

func newMemoryModules(vm *ekuma.VM, sources map[string]string) *memoryModules {
	l := &memoryModules{
		vm:      vm,
		sources: sources,
		modules: map[string]*ekuma.Module{},
		loads:   map[string]int{},
	}
	vm.SetModuleLoader(l.load)
	return l
}

func (l *memoryModules) load(req ekuma.ModuleRequest) (*ekuma.Module, error) {
	specifier := req.Specifier
	l.loads[specifier]++
	if m, ok := l.modules[specifier]; ok {
		return m, nil
	}
	src, ok := l.sources[specifier]
	if !ok {
		return nil, fmt.Errorf("no module %q", specifier)
	}
	m, err := l.vm.ParseModule(specifier, src)
	if err != nil {
		return nil, err
	}
	l.modules[specifier] = m
	return m, nil
}

// run runs the module name and returns the value of the global variable result
// after the evaluation completed.
func (l *memoryModules) run(t *testing.T, name string) ekuma.Value {
	t.Helper()
	m, err := l.load(ekuma.ModuleRequest{Specifier: name})
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Fatalf("the evaluation is %v: %v", st, r)
	}
	v, err := l.vm.Global().Get(t.Context(), "result")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestModules(t *testing.T) {
	tests := []struct {
		name    string
		sources map[string]string
		want    string
	}{
		{
			// An import is a live binding: it sees the later assignments of the exporter.
			name: "live binding",
			sources: map[string]string{
				"main": `import { count, increment } from "counter";
					const seen = [count];
					increment();
					seen.push(count);
					globalThis.result = seen.join();`,
				"counter": `export let count = 0;
					export function increment() { count++; }`,
			},
			want: "0,1",
		},
		{
			name: "assignment before an exported const is initialized",
			sources: map[string]string{
				"main": `import { value as alias } from "main";
					const errors = [];
					for (const assign of [() => alias = 1, () => eval("alias = 1"), () => value = 1]) {
						try { assign(); } catch (e) { errors.push(e.constructor.name); }
					}
					export const value = 2;
					globalThis.result = errors.join();`,
			},
			want: "TypeError,TypeError,ReferenceError",
		},
		{
			// a finished its asynchronous evaluation as a dependency of c before import()
			// asks to evaluate it.
			name: "import of an asynchronous module evaluated as a dependency",
			sources: map[string]string{
				"main": `import "c";
					globalThis.result = String((await import("a")).x);`,
				"c": `import "a";`,
				"a": `export let x = 1; await null; x = 2;`,
			},
			want: "2",
		},
		{
			// b runs first and calls a function of a before a's body ran: the function was
			// instantiated when a was linked.
			name: "cycle with a hoisted function",
			sources: map[string]string{
				"a": `import { fromB } from "b";
					export function greet(s) { return "hello " + s; }
					globalThis.result = fromB;`,
				"b": `import { greet } from "a";
					export const fromB = greet("b");`,
			},
			want: "hello b",
		},
		{
			name: "cycle with an exported deferred namespace",
			sources: map[string]string{
				"main": `import { ns, read } from "a";
					const fromB = read();
					globalThis.result = [ns === fromB, globalThis.runs ?? 0, fromB.x, ns.x, globalThis.runs].join();`,
				"a": `import { read } from "b";
					import defer * as ns from "dep";
					export { ns, read };`,
				"b": `import { ns } from "a";
					export function read() { return ns; }`,
				"dep": `globalThis.runs = (globalThis.runs ?? 0) + 1;
					export const x = 42;`,
			},
			want: "true,0,42,42,1",
		},
		{
			// A module that two modules import, and that a dynamic import loads again,
			// runs once, and all of them see the same namespace object.
			name: "identity",
			sources: map[string]string{
				"main": `import * as ns from "shared";
					import "other";
					globalThis.result = import("shared").then(ns2 =>
						[globalThis.runs, ns === ns2, ns === globalThis.fromOther].join());`,
				"other": `import * as ns from "shared";
					globalThis.fromOther = ns;`,
				"shared": `globalThis.runs = (globalThis.runs ?? 0) + 1;`,
			},
			want: "1,true,true",
		},
		{
			// The decorators of an exported class may precede or follow export.
			name: "decorated exports",
			sources: map[string]string{
				"main": `import D, { A, B } from "dep";
					globalThis.result = [A.tag, B.tag, D.tag].join();`,
				"dep": `function tag(v, c) { v.tag = c.name; }
					@tag export class A {}
					export @tag class B {}
					export default @tag class {}`,
			},
			want: "A,B,default",
		},
		{
			// The modules of a dynamic import evaluate after the importing module.
			name: "dynamic import",
			sources: map[string]string{
				"main": `const log = globalThis.log = [];
					const p = import("dep").then(ns => log.push(ns.default));
					log.push("main");
					globalThis.result = p.then(() => log.join());`,
				"dep": `export default "dep";`,
			},
			want: "main,dep",
		},
		{
			// Modules that become ready together evaluate in the order in which they started to
			// wait, including q1 and q2, which wait for leaf only through p1 and p2.
			name: "asynchronous evaluation order",
			sources: map[string]string{
				"main": `import "p1"; import "p2"; import "q1"; import "q2";
					globalThis.result = log.join();`,
				"p1": `import "leaf"; log.push("p1");`,
				"p2": `import "leaf"; log.push("p2");`,
				"q1": `import "p1"; log.push("q1");`,
				"q2": `import "p2"; log.push("q2");`,
				"leaf": `globalThis.log = [];
					await null;`,
			},
			want: "p1,p2,q1,q2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var vm ekuma.VM
			l := newMemoryModules(&vm, tt.sources)
			names := []string{"main", "a"}
			name := names[0]
			if _, ok := tt.sources[name]; !ok {
				name = names[1]
			}
			v := l.run(t, name)
			if v.Type() == ekuma.TypeObject {
				// A promise of the result.
				var got ekuma.Value
				f := vm.FuncOf("", func(this ekuma.Value, args []ekuma.Value) (any, error) {
					got = args[0]
					return nil, nil
				})
				if _, err := v.Call(t.Context(), "then", f); err != nil {
					t.Fatal(err)
				}
				v = got
			}
			if got := v.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestModuleLoadedOnce checks that the loader is asked once for a request of a referrer
// that loaded.
func TestModuleLoadedOnce(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"main": `globalThis.result = Promise.all([import("dep"), import("dep")]).then(([a, b]) => a === b);`,
		"dep":  `export {};`,
	})
	v := l.run(t, "main")
	var same ekuma.Value
	f := vm.FuncOf("", func(this ekuma.Value, args []ekuma.Value) (any, error) {
		same = args[0]
		return nil, nil
	})
	if _, err := v.Call(t.Context(), "then", f); err != nil {
		t.Fatal(err)
	}
	if !same.Truthy() {
		t.Errorf("the two imports gave different namespace objects")
	}
	if got := l.loads["dep"]; got != 1 {
		t.Errorf("the loader was called %d times for dep, want 1", got)
	}
}

func TestDynamicImportAttributeGetterDeletesNextProperty(t *testing.T) {
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if got := req.Attributes; len(got) != 1 || got["type"] != "json" {
			t.Errorf("loader attributes = %v, want only type=json", got)
		}
		return vm.ParseJSONModule(req.Specifier, `"loaded"`)
	})
	p, err := runScript(t.Context(), &vm, "main", `
		import("data.json", { with: {
			get type() { delete this.extra; return "json"; },
			extra: "unused",
		} }).then(ns => ns.default)`)
	if err != nil {
		t.Fatal(err)
	}
	if st, v := p.PromiseState(); st != ekuma.PromiseFulfilled || v.String() != "loaded" {
		t.Errorf("import result = %v with %v, want fulfilled with loaded", st, v)
	}
}

// TestDynamicImportLoadError checks that an error of the loader rejects the promise of a
// dynamic import with the thrown value, and that a later import asks the loader again.
func TestDynamicImportLoadError(t *testing.T) {
	var vm ekuma.VM
	thrown, err := runScript(t.Context(), &vm, "thrown", `({})`)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		calls++
		return nil, &ekuma.ScriptError{Value: thrown}
	})
	if err := vm.Global().Set(t.Context(), "thrown", thrown); err != nil {
		t.Fatal(err)
	}
	if _, err := runScript(t.Context(), &vm, "main", `
		var result = [];
		import("missing").catch(e => result.push(e === thrown));
		import("missing").catch(e => result.push(e === thrown));`); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "check", `result.join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 2 {
		t.Errorf("the loader was called %d times, want 2", calls)
	}
}

// TestModuleReferrer checks that the loader gets the name of the script or module that
// contains the import, including from eval code and from a function made by Function.
func TestModuleReferrer(t *testing.T) {
	var vm ekuma.VM
	var referrers []string
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		referrers = append(referrers, req.ReferrerName+":"+req.Specifier)
		if req.Specifier == "mod" {
			return vm.ParseModule("mod", `import("fromModule"); eval('import("fromEval")');`)
		}
		return nil, errors.New("no module")
	})
	if _, err := runScript(t.Context(), &vm, "script", `
		import("mod");
		(0, eval)('import("fromIndirectEval")');
		Function('import("fromFunction")')();`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"script:mod",
		"script:fromIndirectEval",
		"script:fromFunction",
		"mod:fromModule",
		"mod:fromEval",
	}
	if fmt.Sprint(referrers) != fmt.Sprint(want) {
		t.Errorf("got %q, want %q", referrers, want)
	}
}

// TestShadowRealmImportValue checks that each ShadowRealm loads a module for itself, once,
// in its own realm.
func TestShadowRealmImportValue(t *testing.T) {
	var vm ekuma.VM
	realms := map[*ekuma.Realm]bool{}
	calls := 0
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		calls++
		if req.Realm == vm.InitialRealm() {
			t.Errorf("the request of a ShadowRealm has the initial realm")
		}
		realms[req.Realm] = true
		return req.Realm.ParseModule(req.Specifier, `globalThis.count = (globalThis.count ?? 0) + 1; export const count = globalThis.count;`)
	})
	if _, err := runScript(t.Context(), &vm, "main", `
		var result;
		const a = new ShadowRealm(), b = new ShadowRealm();
		Promise.all([a.importValue("mod", "count"), a.importValue("mod", "count"), b.importValue("mod", "count")]).then(v => result = v.join());`); err != nil {
		t.Fatal(err)
	}
	v, err := runScript(t.Context(), &vm, "check", `result`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "1,1,1"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if calls != 2 {
		t.Errorf("the loader was called %d times, want 2", calls)
	}
	if len(realms) != 2 {
		t.Errorf("the requests had %d realms, want 2", len(realms))
	}
}

func TestModuleLoaderJobBoundary(t *testing.T) {
	loaderErr := errors.New("loader failed")
	var vm ekuma.VM
	var reported bool
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		reported = true
	})
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		_, err := runScript(t.Context(), &vm, "loader", `Promise.reject("loader");`)
		if err != nil {
			t.Fatal(err)
		}
		if reported {
			t.Error("jobs ran inside the module loader")
		}
		return nil, loaderErr
	})
	m, err := vm.ParseModule("main", `import "dep";`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.LoadRequestedModules(); !errors.Is(err, loaderErr) {
		t.Errorf("LoadRequestedModules: got %v, want the loader error", err)
	}
	if !reported {
		t.Error("jobs did not run after the module loader failed")
	}
}

func TestInternalErrorModule(t *testing.T) {
	var vm ekuma.VM
	cause := errors.New("broken data")
	if err := vm.Global().Set(t.Context(), "fail", ekuma.InternalFailureFunc(&vm, cause)); err != nil {
		t.Fatal(err)
	}
	m, err := vm.ParseModule("failure", `fail();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); !errors.Is(err, cause) {
		t.Errorf("Evaluate: %v, want cause", err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state, reason := p.PromiseState()
	if state != ekuma.PromiseRejected {
		t.Errorf("promise state: %v, want rejected", state)
		return
	}
	var internal *ekuma.InternalError
	exception := &ekuma.ScriptError{
		Value: reason,
	}
	if err := exception.Unwrap(); !errors.As(err, &internal) || !errors.Is(err, cause) {
		t.Errorf("rejection cause: %T %v, want InternalError wrapping cause", err, err)
	}
}

// TestHostPanicReentrantModule checks that a recovered panic cannot expose a partial module.
func TestHostPanicReentrantModule(t *testing.T) {
	var vm ekuma.VM
	marker := errors.New("host panic")
	if err := vm.Global().Set(t.Context(), "bad", vm.FuncOf("bad", func(ekuma.Value, []ekuma.Value) (any, error) {
		panic(marker)
	})); err != nil {
		t.Fatal(err)
	}
	m, err := vm.ParseModule("abandoned", `bad()`)
	if err != nil {
		t.Fatal(err)
	}
	vm.SetModuleLoader(func(ekuma.ModuleRequest) (*ekuma.Module, error) { return m, nil })
	importer, err := vm.ParseModule("importer", `import "abandoned"; globalThis.importerRan = true;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Global().Set(t.Context(), "recoverModule", vm.FuncOf("recoverModule", func(ekuma.Value, []ekuma.Value) (any, error) {
		if got := recoverHostPanic(func() { _, _ = m.Evaluate(t.Context()) }); got != marker {
			t.Errorf("panic = %v, want original marker", got)
			return nil, nil
		}
		p, err := importer.Evaluate(t.Context())
		if err != nil {
			t.Errorf("Evaluate importer: %v", err)
			return nil, nil
		}
		state, reason := p.PromiseState()
		if state != ekuma.PromiseRejected {
			t.Errorf("importer state = %v, want rejected", state)
		}
		if err := (&ekuma.ScriptError{Value: reason}); !errors.Is(err, marker) {
			t.Errorf("importer error = %v, want original cause", err)
		}
		return nil, nil
	})); err != nil {
		t.Fatal(err)
	}
	if got := run(t, &vm, `var importerRan = false; recoverModule(); importerRan`); got != "false" {
		t.Errorf("importer ran against partial module: %s", got)
	}
}

// TestCancelModule checks that a context stops a module whose top level loops forever.
func TestCancelModule(t *testing.T) {
	errStop := errors.New("stop")
	for _, src := range []string{
		`for (;;) {}`,
		`await null; for (;;) {}`,
	} {
		for _, cause := range []error{context.DeadlineExceeded, errStop} {
			var vm ekuma.VM
			m, err := vm.ParseModule("endless", src)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeoutCause(t.Context(), 20*time.Millisecond, cause)
			if _, err := m.Evaluate(ctx); !errors.Is(err, cause) {
				t.Errorf("%s: got %v, want %v", src, err, cause)
			}
			cancel()

			// The interruption evaluates the module with an Error made from the cause, which an
			// importer gets too.
			vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
				return m, nil
			})
			importer, err := vm.ParseModule("importer", `import "endless";`)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range []*ekuma.Module{m, importer} {
				p, err := m.Evaluate(t.Context())
				if err != nil {
					t.Fatalf("%s: Evaluate %s: %v", src, m.Name(), err)
				}
				st, r := p.PromiseState()
				if st != ekuma.PromiseRejected {
					t.Errorf("%s: %s: got %v, want rejected", src, m.Name(), st)
					continue
				}
				if err := (&ekuma.ScriptError{Value: r}); !errors.Is(err, cause) {
					t.Errorf("%s: %s: got reason %v, want an Error made from %v", src, m.Name(), err, cause)
				}
			}
		}
	}
}

// TestModuleSteps checks that the steps of a module can be taken in any order and again.
func TestModuleSteps(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"dep": `export let v = "dep";`,
	})
	m, err := vm.ParseModule("main", `export { v } from "dep";`)
	if err != nil {
		t.Fatal(err)
	}

	// Namespace loads and links m first.
	ns, err := m.Namespace()
	if err != nil {
		t.Fatalf("Namespace: %v", err)
	}
	var ex *ekuma.ScriptError
	if _, err := ns.Get(t.Context(), "v"); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "ReferenceError") {
		t.Errorf("reading an export before the evaluation: got %v, want a ReferenceError", err)
	}
	if err := m.LoadRequestedModules(); err != nil {
		t.Errorf("LoadRequestedModules: %v", err)
	}
	if err := m.Link(); err != nil {
		t.Errorf("Link: %v", err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, _ := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Errorf("got %v, want fulfilled", st)
	}
	p2, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !p2.Equal(p) {
		t.Errorf("a second Evaluate returned another promise")
	}
	if err := m.Link(); err != nil {
		t.Errorf("Link after Evaluate: %v", err)
	}
	ns2, err := m.Namespace()
	if err != nil {
		t.Fatalf("Namespace: %v", err)
	}
	if !ns2.Equal(ns) {
		t.Errorf("a second Namespace returned another object")
	}
	if v, err := ns.Get(t.Context(), "v"); err != nil || v.String() != "dep" {
		t.Errorf("got %v, %v, want dep", v, err)
	}
	if got := l.loads["dep"]; got != 1 {
		t.Errorf("the loader was called %d times for dep, want 1", got)
	}
}

// TestModuleEvaluationError checks that an exception of a module rejects the promise of
// Evaluate, and that evaluating the module again returns the same promise.
func TestModuleEvaluationError(t *testing.T) {
	var vm ekuma.VM
	m, err := vm.ParseModule("main", `throw "thrown";`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseRejected || r.String() != "thrown" {
		t.Errorf("got %v with %v, want rejected with thrown", st, r)
	}
	p2, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !p2.Equal(p) {
		t.Errorf("a second Evaluate returned another promise")
	}
}

func TestDeferredModuleRejectionReporting(t *testing.T) {
	var vm ekuma.VM
	var reported []ekuma.Value
	vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
		reported = append(reported, promise)
	})
	modules := newMemoryModules(&vm, map[string]string{
		"dep": `throw "deferred";`,
		"main": `import defer * as ns from "dep";
			try { ns.x; } catch (e) { globalThis.result = Promise.reject(e); }`,
	})
	want := modules.run(t, "main")
	if len(reported) != 1 || !reported[0].Equal(want) {
		t.Errorf("reported promises = %v, want only the promise created in catch", reported)
	}
}

// TestModuleLoaderErrors checks the error that each kind of error of the loader becomes.
func TestModuleLoaderErrors(t *testing.T) {
	var vm ekuma.VM
	thrown := vm.ValueOf(map[string]any{})
	tests := []struct {
		name string
		err  error

		// ctor is the constructor of the thrown value, or empty for thrown itself.
		ctor string
	}{
		{
			name: "ScriptError",
			err:  &ekuma.ScriptError{Value: thrown},
		},
		{
			name: "SyntaxError",
			err: &ekuma.SyntaxError{
				File:    "dep",
				Line:    1,
				Message: "bad",
			},
			ctor: "SyntaxError",
		},
		{
			name: "other",
			err:  errors.New("no module"),
			ctor: "Error",
		},
		{
			name: "no loader",
			ctor: "TypeError",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err != nil {
				vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
					return nil, tt.err
				})
			} else {
				vm.SetModuleLoader(nil)
			}
			m, err := vm.ParseModule("main", `import "dep";`)
			if err != nil {
				t.Fatal(err)
			}
			var ex *ekuma.ScriptError
			if err := m.LoadRequestedModules(); !errors.As(err, &ex) {
				t.Fatalf("got %v, want a *ScriptError", err)
			}
			if tt.ctor == "" {
				if !ex.Value.Equal(thrown) {
					t.Errorf("got %v, want the value of the *ScriptError", ex.Value)
				}
				return
			}
			c, err := ex.Value.Get(t.Context(), "constructor")
			if err != nil {
				t.Fatal(err)
			}
			if n, err := c.Get(t.Context(), "name"); err != nil || n.String() != tt.ctor {
				t.Errorf("got an instance of %v, want %s", n, tt.ctor)
			}
		})
	}
}

func TestModuleLoaderErrorRealm(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	dep, err := r.ParseModule("dep", `import "missing";`)
	if err != nil {
		t.Fatal(err)
	}
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if req.Specifier == "dep" {
			return dep, nil
		}
		return nil, errors.New("missing module")
	})
	if err := vm.Global().Set(t.Context(), "other", r.Global()); err != nil {
		t.Fatal(err)
	}
	run(t, &vm, `var reason; import("dep").catch(e => { reason = e; });`)
	if got := run(t, &vm, `Object.getPrototypeOf(reason) === other.Error.prototype`); got != "true" {
		t.Error("the loader error did not inherit from the importing module's Error.prototype")
	}
}

// TestModuleLinkRetry checks that a Link that failed can be taken again, and that an import
// that does not resolve is an error of Link.
func TestModuleLinkRetry(t *testing.T) {
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return nil, errors.New("not yet")
	})
	m, err := vm.ParseModule("main", `import { v } from "dep"; globalThis.result = v;`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Link(); err == nil {
		t.Fatal("Link succeeded with a failing loader")
	}
	var referrer *ekuma.Module
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		referrer = req.Referrer
		return vm.ParseModule(req.Specifier, `export const v = "linked";`)
	})
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if st, r := p.PromiseState(); st != ekuma.PromiseFulfilled {
		t.Errorf("got %v with %v, want fulfilled", st, r)
	}
	if referrer != m {
		t.Errorf("the Referrer of the request is not the importing module")
	}

	bad, err := vm.ParseModule("bad", `import { missing } from "dep2";`)
	if err != nil {
		t.Fatal(err)
	}
	var ex *ekuma.ScriptError
	if err := bad.Link(); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("got %v, want a SyntaxError", err)
	}
}

// TestModuleLoaderReentry checks that linking a module from inside the loader that loads it
// is an error rather than a recursion.
func TestModuleLoaderReentry(t *testing.T) {
	var vm ekuma.VM
	var reentryErr error
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		reentryErr = req.Referrer.Link()
		return vm.ParseModule(req.Specifier, `export const v = 1;`)
	})
	m, err := vm.ParseModule("main", `import { v } from "dep";`)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Link(); err != nil {
		t.Errorf("Link: %v", err)
	}
	var ex *ekuma.ScriptError
	if !errors.As(reentryErr, &ex) || !isInstance(t, &vm, ex.Value, "TypeError") {
		t.Errorf("got %v from the re-entrant Link, want a TypeError", reentryErr)
	}
}

// TestModuleOfAnotherVM checks that a loader that returns a Module of another VM panics.
func TestModuleOfAnotherVM(t *testing.T) {
	var vm, other ekuma.VM
	dep := other.NewDefaultExportSyntheticModule("dep", ekuma.Undefined)
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return dep, nil
	})
	m, err := vm.ParseModule("main", `import "dep";`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Errorf("LoadRequestedModules did not panic")
		}
	}()
	_ = m.LoadRequestedModules()
}

// TestSyntheticModules checks the modules of NewDefaultExportSyntheticModule and ParseJSONModule.
func TestSyntheticModules(t *testing.T) {
	var vm ekuma.VM
	syn := vm.NewDefaultExportSyntheticModule("syn", vm.ValueOf("synthetic"))
	j, err := vm.ParseJSONModule("data.json", `{"a": [1, 2]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := j.Name(), "data.json"; got != want {
		t.Errorf("got name %q, want %q", got, want)
	}
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		if req.Attributes["type"] == "json" {
			return j, nil
		}
		return syn, nil
	})
	m, err := vm.ParseModule("main", `import s from "syn";
		import j from "data.json" with { type: "json" };
		globalThis.result = s + " " + j.a.join();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := vm.Global().Get(t.Context(), "result")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "synthetic 1,2"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	var ex *ekuma.ScriptError
	if _, err := vm.ParseJSONModule("bad.json", `{`); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("got %v, want a SyntaxError", err)
	}

	// Evaluating a synthetic or JSON module directly returns a fulfilled promise of its realm.
	for _, tc := range []struct {
		name string
		mod  *ekuma.Module
	}{
		{"synthetic", syn},
		{"json", j},
	} {
		p, err := tc.mod.Evaluate(t.Context())
		if err != nil {
			t.Fatalf("%s Evaluate: %v", tc.name, err)
		}
		if st, _ := p.PromiseState(); st != ekuma.PromiseFulfilled {
			t.Errorf("%s Evaluate state = %v, want fulfilled", tc.name, st)
		}
		if !isInstance(t, &vm, p, "Promise") {
			t.Errorf("%s Evaluate returned promise from wrong realm", tc.name)
		}
	}
	other := vm.NewRealm()
	otherSyn := other.NewDefaultExportSyntheticModule("other-syn", other.ValueOf(1))
	otherJSON, err := other.ParseJSONModule("other.json", `{"b": 2}`)
	if err != nil {
		t.Fatal(err)
	}
	otherPromiseCtor, err := other.Global().Get(t.Context(), "Promise")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mod  *ekuma.Module
	}{
		{"other synthetic", otherSyn},
		{"other json", otherJSON},
	} {
		p, err := tc.mod.Evaluate(t.Context())
		if err != nil {
			t.Fatalf("%s Evaluate: %v", tc.name, err)
		}
		if st, _ := p.PromiseState(); st != ekuma.PromiseFulfilled {
			t.Errorf("%s Evaluate state = %v, want fulfilled", tc.name, st)
		}
		if ok, err := p.InstanceOf(t.Context(), otherPromiseCtor); err != nil || !ok {
			t.Errorf("%s Evaluate promise instanceof other.Promise = %v, %v", tc.name, ok, err)
		}
	}
}

func TestSyntheticModuleNamedExports(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	exports := map[string]ekuma.Value{
		"answer":       r.ValueOf(42),
		"default":      r.ValueOf("default export"),
		"display-name": r.ValueOf("native"),
		"greet": r.FuncOf("greet", func(_ ekuma.Value, _ []ekuma.Value) (any, error) {
			return "hello", nil
		}),
		"state": vm.ValueOf(map[string]any{}),
	}
	native := r.NewSyntheticModule("native", exports)
	exports["answer"] = r.ValueOf(-1)
	exports["extra"] = ekuma.Undefined
	barrel, err := vm.ParseModule("barrel", `export { state as alias } from "native";`)
	if err != nil {
		t.Fatal(err)
	}
	modules := map[string]*ekuma.Module{
		"native": native,
		"barrel": barrel,
	}
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return modules[req.Specifier], nil
	})
	m, err := vm.ParseModule("main", `
		import d, { answer, greet, state, "display-name" as name } from "native";
		import * as ns from "native";
		import { alias } from "barrel";
		const dynamic = await import("native");
		export const values = [d, answer, greet(), name].join(",");
		export const keys = Object.keys(ns).join(",");
		export const same = state === dynamic.state && state === alias && ns === dynamic;
		export const sameRealm = state instanceof Object;
	`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := m.Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if state, reason := p.PromiseState(); state != ekuma.PromiseFulfilled {
		t.Fatalf("Evaluate returned %v promise: %v", state, reason)
	}
	ns, err := m.Namespace()
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]any{
		"values":    "default export,42,hello,native",
		"keys":      "answer,default,display-name,greet,state",
		"same":      true,
		"sameRealm": true,
	} {
		v, err := ns.Get(t.Context(), name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if !v.Equal(vm.ValueOf(want)) {
			t.Errorf("%s = %v, want %v", name, v, want)
		}
	}
}

func TestSyntheticModuleMissingExport(t *testing.T) {
	var vm ekuma.VM
	native := vm.NewSyntheticModule("native", map[string]ekuma.Value{
		"answer": vm.ValueOf(42),
	})
	vm.SetModuleLoader(func(ekuma.ModuleRequest) (*ekuma.Module, error) {
		return native, nil
	})
	m, err := vm.ParseModule("main", `import missing from "native";`)
	if err != nil {
		t.Fatal(err)
	}
	var ex *ekuma.ScriptError
	if err := m.Link(); !errors.As(err, &ex) || !isInstance(t, &vm, ex.Value, "SyntaxError") {
		t.Errorf("Link error = %v, want a SyntaxError", err)
	}
}

func TestSyntheticModuleInvalidExportName(t *testing.T) {
	var vm ekuma.VM
	defer func() {
		if recover() == nil {
			t.Error("NewSyntheticModule accepted an invalid UTF-8 export name")
		}
	}()
	vm.NewSyntheticModule("native", map[string]ekuma.Value{
		"\xff": ekuma.Undefined,
	})
}

func TestModuleLinkRealm(t *testing.T) {
	t.Run("direct Link in secondary realm", func(t *testing.T) {
		var vm ekuma.VM
		r := vm.NewRealm()
		m, err := r.ParseModule("m.js", `
			export function f() {}
			export const ok = (f instanceof Function);
		`)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.Link(); err != nil {
			t.Fatalf("Link: %v", err)
		}
		if _, err := m.Evaluate(t.Context()); err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		ns, err := m.Namespace()
		if err != nil {
			t.Fatalf("Namespace: %v", err)
		}
		ok, err := ns.Get(t.Context(), "ok")
		if err != nil {
			t.Fatal(err)
		}
		if !ok.Bool() {
			t.Error("hoisted function in secondary realm module had wrong Function.prototype")
		}
	})

	t.Run("cross-realm import links dependency in its own realm", func(t *testing.T) {
		var vm ekuma.VM
		r := vm.NewRealm()
		dep, err := r.ParseModule("dep.js", `
			export function f() {}
			export const ok = (f instanceof Function);
		`)
		if err != nil {
			t.Fatal(err)
		}
		vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
			return dep, nil
		})
		mainMod, err := vm.ParseModule("main.js", `
			import { ok } from "dep.js";
			globalThis.ok = ok;
		`)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mainMod.Evaluate(t.Context()); err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		ok, err := vm.Global().Get(t.Context(), "ok")
		if err != nil {
			t.Fatal(err)
		}
		if !ok.Bool() {
			t.Error("cross-realm imported module linked hoisted function in importer's realm")
		}
	})
}

func TestModuleSource(t *testing.T) {
	var vm ekuma.VM
	src := ekuma.NewSourceModule(vm.InitialRealm(), "src", "TestModuleSource")
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		return src, nil
	})
	m, err := vm.ParseModule("main", `import source s from "src";
		const d = await import.source("src");
		globalThis.result = (s === d) + " " + Object.prototype.toString.call(s);`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := vm.Global().Get(t.Context(), "result")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true [object TestModuleSource]"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestRealmModules checks that modules belong to their realms, and that the module loader is
// told the realm of each import.
func TestRealmModules(t *testing.T) {
	var vm ekuma.VM
	r := vm.NewRealm()
	got := map[string]*ekuma.Realm{}
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		got[req.ReferrerName+":"+req.Specifier] = req.Realm
		if req.Specifier == "data" {
			return req.Realm.ParseJSONModule("data", `{}`)
		}
		return req.Realm.ParseModule(req.Specifier, `export default globalThis;`)
	})
	main, err := r.ParseModule("main", `
		import g from "lib";
		import data from "data" with { type: "json" };
		export const ok = g === globalThis && Object.getPrototypeOf(data) === Object.prototype;
		export const dynamic = import("dyn");`)
	if err != nil {
		t.Fatalf("ParseModule: %v", err)
	}
	if _, err := main.Evaluate(t.Context()); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	ns, err := main.Namespace()
	if err != nil {
		t.Fatalf("Namespace: %v", err)
	}
	if ok, err := ns.Get(t.Context(), "ok"); err != nil || !ok.Bool() {
		t.Errorf("the imports of a module of a realm do not belong to the realm: %v, %v", ok, err)
	}
	if _, err := runScript(t.Context(), &vm, "script", `import("fromDefault")`); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	for k, want := range map[string]*ekuma.Realm{
		"main:lib":           r,
		"main:data":          r,
		"main:dyn":           r,
		"script:fromDefault": vm.InitialRealm(),
	} {
		if got[k] != want {
			t.Errorf("the realm of the request %s is not the expected one", k)
		}
	}
}

// TestImportMeta checks that import.meta has the properties that the host gives it.
func TestImportMeta(t *testing.T) {
	var vm ekuma.VM
	l := newMemoryModules(&vm, map[string]string{
		"main": `globalThis.result = import.meta.url;`,
	})
	vm.SetImportMeta(func(m *ekuma.Module) map[string]any {
		return map[string]any{
			"url": "memory:main",
		}
	})
	if got, want := l.run(t, "main").String(), "memory:main"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestImportMetaReentry(t *testing.T) {
	for _, panicOnce := range []bool{false, true} {
		t.Run(fmt.Sprintf("panic=%t", panicOnce), func(t *testing.T) {
			var vm ekuma.VM
			l := newMemoryModules(&vm, map[string]string{
				"main": `export function meta() { return import.meta; } globalThis.result = meta;`,
			})
			meta := l.run(t, "main")
			marker := new(int)
			var inner ekuma.Value
			var calls int
			vm.SetImportMeta(func(*ekuma.Module) map[string]any {
				calls++
				if calls == 1 {
					var err error
					inner, err = meta.Invoke(t.Context())
					if err != nil {
						t.Errorf("reentrant meta(): %v", err)
					}
					if panicOnce {
						panic(marker)
					}
				}
				return map[string]any{"self": inner}
			})
			wantCalls := 1
			if panicOnce {
				if got := recoverHostPanic(func() { _, _ = meta.Invoke(t.Context()) }); got != marker {
					t.Errorf("panic = %v, want original marker", got)
				}
				wantCalls++
			}
			if got := run(t, &vm, `const meta = result; meta() === meta().self`); got != "true" {
				t.Error("import.meta.self did not refer to the outer call's object")
			}
			if calls != wantCalls {
				t.Errorf("import.meta hook ran %d times, want %d", calls, wantCalls)
			}
		})
	}
}

// TestLongImportChain checks that loading, linking and evaluating a long chain of imports
// does not exhaust the Go stack.
func TestLongImportChain(t *testing.T) {
	const n = 20000
	var vm ekuma.VM
	vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
		var i int
		if _, err := fmt.Sscanf(req.Specifier, "m%d", &i); err != nil {
			return nil, err
		}
		src := fmt.Sprintf(`export * from "m%d"; export const v%d = %d;`, i+1, i, i)
		if i == n {
			src = fmt.Sprintf(`export const v%d = %d;`, i, i)
		}
		return vm.ParseModule(req.Specifier, src)
	})
	m, err := vm.ParseModule("main", fmt.Sprintf(`import { v%d } from "m0"; globalThis.result = v%d;`, n, n))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Evaluate(t.Context()); err != nil {
		t.Fatal(err)
	}
	v, err := vm.Global().Get(t.Context(), "result")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Int(); got != n {
		t.Errorf("got %d, want %d", got, n)
	}
}

func TestUnboundModule(t *testing.T) {
	const name = "main.js"
	u, err := ekuma.ParseUnboundModule(name, `
		import prefix from "./prefix.js";
		function next() {
			globalThis.count = (globalThis.count ?? 0) + 1;
			return prefix + globalThis.count;
		}
		export const message = next();
	`)
	if err != nil {
		t.Fatal(err)
	}

	for _, prefix := range []string{"A", "B"} {
		t.Run(prefix, func(t *testing.T) {
			t.Parallel()
			var vm ekuma.VM
			r := vm.NewRealm()
			dep := r.NewDefaultExportSyntheticModule("prefix.js", r.ValueOf(prefix))
			var m *ekuma.Module
			vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
				if req.Realm != r || req.Referrer != m || req.ReferrerName != name || req.Specifier != "./prefix.js" {
					t.Errorf("unexpected module request: %+v", req)
				}
				return dep, nil
			})
			for i := 1; i <= 2; i++ {
				m = r.BindModule(u)
				p, err := m.Evaluate(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				if state, reason := p.PromiseState(); state != ekuma.PromiseFulfilled {
					t.Fatalf("module evaluation %v: %v", state, reason)
				}
				ns, err := m.Namespace()
				if err != nil {
					t.Fatal(err)
				}
				message, err := ns.Get(t.Context(), "message")
				if err != nil {
					t.Fatal(err)
				}
				if got, want := message.String(), prefix+strconv.Itoa(i); got != want {
					t.Errorf("binding %d: got %q, want %q", i, got, want)
				}
			}
		})
	}
}

func TestTest262ModuleJobs(t *testing.T) {
	synctest.Test(t, test262Runner.CheckModuleJobs)
}

func TestTest262ModuleRealms(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"marker_FIXTURE.js": "export const marker = globalThis.marker;",
		"marker.js": `/*---
flags: [module]
---*/
globalThis.marker = "outer";
assert.sameValue((await import("./marker_FIXTURE.js")).marker, "outer");
const shadow = new ShadowRealm();
shadow.evaluate('globalThis.marker = "inner"');
assert.sameValue(await shadow.importValue("./marker_FIXTURE.js", "marker"), "inner");
shadow.evaluate('globalThis.marker = "changed"');
assert.sameValue(await shadow.importValue("././marker_FIXTURE.js", "marker"), "inner");`,
		"data_FIXTURE.json": "[42]",
		"typed.js": `/*---
flags: [module]
---*/
const other = $262.createRealm();
const load = other.evalScript('(type => import("./data_FIXTURE.json", { with: { type } }))');
assert.sameValue(Object.getPrototypeOf((await load("json")).default), other.global.Array.prototype);
assert.sameValue(Object.getPrototypeOf((await load("bytes")).default), other.global.Uint8Array.prototype);
assert.sameValue((await load("text")).default, "[42]");`,
		"source.js": `/*---
flags: [module]
---*/
import source initial from "<module source>";
const other = $262.createRealm();
const src = await other.evalScript('import.source("<module source>")');
assert.sameValue(src === initial, false);
assert.sameValue(src instanceof other.global.$262.AbstractModuleSource, true);
assert.sameValue(await other.evalScript('import.source("<module source>")'), src);`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	test262Runner.RunScripts(t, dir)
}

// TestLargeModuleGraphs imports module graphs whose handling took time quadratic in their
// sizes, so that the import missed a deadline that the linear handling meets by far.
func TestLargeModuleGraphs(t *testing.T) {
	if raceEnabled {
		t.Skip("the graphs are too large for the race detector")
	}
	for _, tc := range []struct {
		name string
		n    int

		// src returns the source text of the module mi; m1 is the one imported. The modules
		// whose sources are empty are synthetic.
		src func(n, i int) string

		// want is what the root module computes from the namespace ns of m1.
		want string
		expr string
	}{
		{
			// The requests of m1 are told apart while it is parsed, and its dependencies
			// are listed while it is evaluated.
			name: "many requests",
			n:    240000,
			src: func(n, i int) string {
				if i != 1 {
					return ""
				}
				var b strings.Builder
				for j := 2; j < n; j++ {
					fmt.Fprintf(&b, "import 'm%d';\n", j)
				}
				b.WriteString("export const done = true;")
				return b.String()
			},
			expr: "ns.done",
			want: "true",
		},
		{
			name: "many indirect exports",
			n:    80000,
			src: func(n, i int) string {
				var b strings.Builder
				for j := range n {
					if i == 1 {
						fmt.Fprintf(&b, "export {e%d as f%d} from 'm2';\n", j, j)
					} else {
						fmt.Fprintf(&b, "export const e%d = %d;\n", j, j)
					}
				}
				return b.String()
			},
			expr: "Object.keys(ns).length + ':' + ns.f12345",
			want: "80000:12345",
		},
		{
			// Each name of the namespace of m1 is exported by a module further down a chain
			// of export * from.
			name: "long chain of export * from",
			n:    16000,
			src: func(n, i int) string {
				if i+1 < n {
					return fmt.Sprintf("export * from 'm%d'; export const v%d = %d;", i+1, i, i)
				}
				return "export const last = 1;"
			},
			expr: "Object.keys(ns).length + ':' + ns.v15000",
			want: "15999:15000",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			loaded := map[string]*ekuma.Module{}
			vm.SetModuleLoader(func(req ekuma.ModuleRequest) (*ekuma.Module, error) {
				if m, ok := loaded[req.Specifier]; ok {
					return m, nil
				}
				i, err := strconv.Atoi(strings.TrimPrefix(req.Specifier, "m"))
				if err != nil {
					return nil, err
				}
				src := tc.src(tc.n, i)
				if src == "" {
					m := vm.NewDefaultExportSyntheticModule(req.Specifier, ekuma.Undefined)
					loaded[req.Specifier] = m
					return m, nil
				}
				m, err := vm.ParseModule(req.Specifier, src)
				if err != nil {
					return nil, err
				}
				loaded[req.Specifier] = m
				return m, nil
			})
			// The dynamic import loads, links and evaluates m1 within the deadline.
			root, err := vm.ParseModule("root", `const ns = await import("m1"); globalThis.result = String(`+tc.expr+`);`)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), linearTimeDeadline())
			defer cancel()
			p, err := root.Evaluate(ctx)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if state, reason := p.PromiseState(); state != ekuma.PromiseFulfilled {
				t.Fatalf("Evaluate: promise %v: %v", state, reason)
			}
			got, err := vm.Global().Get(t.Context(), "result")
			if err != nil {
				t.Fatal(err)
			}
			if got.String() != tc.want {
				t.Errorf("result = %s, want %s", got, tc.want)
			}
		})
	}
}
