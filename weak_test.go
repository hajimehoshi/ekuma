// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	"context"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/hajimehoshi/ekuma"
)

func TestWeakCollectionsCollected(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var k = {}, c = new WeakMap();
		function make() {
			var a = new WeakMap(), b = new WeakMap();
			a.set(k, {}); b.set(k, {}); c.set(k, {n: 3});
			return [a, b, a.get(k), b.get(k)].map(o => new WeakRef(o));
		}
		var refs = make();
	`); err != nil {
		t.Fatal(err)
	}
	// The map cleanups release the values for a subsequent GC. The key stays idle.
	const want = "false,false,false,false"
	var live string
	for range 20 {
		runtime.GC()
		v, err := runScript(t.Context(), &vm, "refs", `refs.map(r => r.deref() !== undefined).join()`)
		if err != nil {
			t.Fatal(err)
		}
		live = v.String()
		if live == want {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if live != want {
		t.Errorf("reachable (map a, map b, value a, value b): got %s, want %s", live, want)
	}
	v, err := runScript(t.Context(), &vm, "reuse", `
		var d = new WeakMap();
		d.set(k, 4);
		[c.get(k).n, d.get(k)].join();
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "3,4"; got != want {
		t.Errorf("after the collections were collected: got %s, want %s", got, want)
	}
}

// TestMovedStorageReleasesValues checks that the storage that the properties or elements of
// an object move out of does not keep their values alive.
func TestMovedStorageReleasesValues(t *testing.T) {
	// The scripts run in order. Each case reads a property or a variable through an inline cache
	// or stores it in the storage that the object is created with, makes the storage move by
	// adding properties or elements, and then drops the value that wr refers to.
	for _, tc := range []struct {
		name    string
		scripts []string
	}{
		{
			name: "global variable",
			scripts: []string{`
				var g = {};
				var wr = new WeakRef(g);
				function read() { return g; }
				read();
				for (var i = 0; i < 200; i++) globalThis["k" + i] = i;
				g = null;
			`},
		},
		{
			// The bindings of a script are declared before it runs, so the bindings that move
			// the storage come in another script.
			name: "global lexical binding",
			scripts: []string{`
				let g = {};
				var wr = new WeakRef(g);
				function read() { return g; }
				read();
			`, `
				let l0, l1, l2, l3, l4, l5, l6, l7, l8, l9, l10, l11, l12, l13, l14, l15, l16;
			`, `
				g = null;
			`},
		},
		{
			name: "prototype property",
			scripts: []string{`
				function F() {}
				F.prototype.m = {};
				var wr = new WeakRef(F.prototype.m);
				function read(o) { return o.m; }
				read(new F());
				for (var i = 0; i < 30; i++) F.prototype["p" + i] = i;
				F.prototype.m = null;
			`},
		},
		{
			name: "object literal",
			scripts: []string{`
				var o = {a: {}};
				var wr = new WeakRef(o.a);
				for (var i = 0; i < 20; i++) o["k" + i] = i;
				o.a = null;
			`},
		},
		{
			name: "array literal",
			scripts: []string{`
				var a = [{}, 1];
				var wr = new WeakRef(a[0]);
				for (var i = 0; i < 20; i++) a.push(i);
				a[0] = null;
			`},
		},
		{
			// The elements and named properties of a small match result share inline storage.
			name: "match result",
			scripts: []string{`
				var m = /a(b)/.exec("ab");
				m[1] = {};
				var wr = new WeakRef(m[1]);
				m.push(0);
				m[1] = null;
			`},
		},
		{
			name: "shrunken match result",
			scripts: []string{`
				var m = new RegExp("()".repeat(80)).exec("");
				m[80] = {};
				var wr = new WeakRef(m[80]);
				m.length = 1;
			`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var vm ekuma.VM
			for i, src := range tc.scripts {
				if _, err := runScript(t.Context(), &vm, "script"+strconv.Itoa(i), src); err != nil {
					t.Fatal(err)
				}
			}
			live := true
			for range 10 {
				runtime.GC()
				v, err := runScript(t.Context(), &vm, "check", `wr.deref() !== undefined`)
				if err != nil {
					t.Fatal(err)
				}
				if live = v.Bool(); !live {
					break
				}
				time.Sleep(time.Millisecond)
			}
			if live {
				t.Error("the dropped value is still reachable")
			}
		})
	}
}

// TestLeftWithScopeReleasesObject checks that a suspended generator does not keep the object of
// a with statement that it left alive.
func TestLeftWithScopeReleasesObject(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var refs = [];
		function make() {
			const o = {};
			refs.push(new WeakRef(o));
			return o;
		}
		// The call reuses the registers of the with statements.
		function reuse() {}
		function* end() {
			with (make()) {}
			reuse(1, 2, 3, 4);
			yield;
		}
		function* unwind() {
			try {
				with (make()) throw 0;
			} catch {}
			reuse(1, 2, 3, 4);
			yield;
		}
		var suspended = [end(), unwind()];
		for (const it of suspended) it.next();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `refs.map(r => r.deref() === undefined).join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true"; got != want {
		t.Errorf("collected objects of the with statements left at the end and by an exception: got %s, want %s", got, want)
	}
}

// TestMappedArgumentsReleaseValues checks that a mapped arguments object does not keep a value
// alive that the parameter no longer has.
func TestMappedArgumentsReleaseValues(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var refs = [];
		function make() {
			const o = {};
			refs.push(new WeakRef(o));
			return o;
		}
		function initial(a) {
			a = null;
			return arguments;
		}
		function stored(a) {
			arguments[0] = make();
			a = null;
			return arguments;
		}
		function refused(a) {
			Object.defineProperty(arguments, 0, {configurable: false});
			Reflect.defineProperty(arguments, 0, {get() {}});
			a = null;
			return arguments;
		}
		var kept = [initial(make()), stored(0), refused(make())];
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `refs.map(r => r.deref() === undefined).join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true,true"; got != want {
		t.Errorf("collected values of the parameter (initial, stored, after a refused definition): got %s, want %s", got, want)
	}
}

// TestSettledPromiseCombinatorsReleaseValues checks that a function that settles an element of
// a settled promise combinator does not keep the values alive.
func TestSettledPromiseCombinatorsReleaseValues(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		function C(executor) { executor(() => {}, () => {}); }
		C.resolve = v => v;
		var kept = [], refs = [];
		function run(combine) {
			const value = {};
			refs.push(new WeakRef(value));
			combine.call(C, [{
				then(onFulfilled, onRejected) {
					const settle = combine === Promise.any ? onRejected : onFulfilled;
					kept.push(settle);
					settle(value);
				},
			}]);
		}
		run(Promise.all);
		run(Promise.allSettled);
		run(Promise.any);
		run(Promise.allKeyed);
		run(Promise.allSettledKeyed);
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `refs.map(r => r.deref() === undefined).join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true,true,true,true"; got != want {
		t.Errorf("collected values of all, allSettled, any, allKeyed and allSettledKeyed: got %s, want %s", got, want)
	}
}

func TestCompletedIteratorHelperReleasesSource(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		function make() {
			const source = [].values();
			return [source.map(Boolean), new WeakRef(source)];
		}
		const [exhausted, afterNext] = make();
		const [closed, afterReturn] = make();
		exhausted.next();
		closed.return();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `
		[afterNext.deref() === undefined, afterReturn.deref() === undefined].join()
	`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true"; got != want {
		t.Errorf("source collected after next and return: got %s, want %s", got, want)
	}
}

// TestIteratorHelpersReleaseUsedInputs keeps the helpers suspended after they have
// finished using one of their inputs.
func TestIteratorHelpersReleaseUsedInputs(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		function concat() {
			const input = {[Symbol.iterator]: Array.prototype.values.bind([])};
			return [Iterator.concat(input, [1]), new WeakRef(input[Symbol.iterator])];
		}
		function zip() {
			const input = {next: () => ({done: true})};
			return [Iterator.zip([input, [1]], {mode: "longest"}), new WeakRef(input.next)];
		}
		function chunks() {
			const input = [1].values();
			return [input.chunks(2), new WeakRef(input)];
		}
		const cases = [concat(), zip(), chunks()];
		for (const [helper] of cases) helper.next();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `cases.map(([, ref]) => ref.deref() === undefined).join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true,true"; got != want {
		t.Errorf("collected inputs of concat, zip and chunks: got %s, want %s", got, want)
	}
}

func TestCompletedGeneratorReleasesActivation(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		function* gen() { yield arguments[0]; }
		function make() {
			const receiver = {}, arg = {}, result = {};
			const it = gen.call(receiver, arg);
			it.next();
			it.return(result);
			return [it, [receiver, arg, result].map(v => new WeakRef(v))];
		}
		const [completed, refs] = make();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `refs.map(r => r.deref() === undefined).join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), "true,true,true"; got != want {
		t.Errorf("collected receiver, argument and return value: got %s, want %s", got, want)
	}
}

func TestCompletedGeneratorReleasesTemplateSite(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		const GeneratorFunction = Object.getPrototypeOf(function* () {}).constructor;
		const completed = GeneratorFunction("tag", "yield tag\x60site\x60")(site => new WeakRef(site));
		const ref = completed.next().value;
		completed.next();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "check", `ref.deref() === undefined`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Error("the completed generator kept its template site alive")
	}
}

func TestWeakMapValueReferencesKey(t *testing.T) {
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var m = new WeakMap();
		var ref = (() => {
			let k = {};
			m.set(k, {key: k});
			return new WeakRef(k);
		})();
	`); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	v, err := runScript(t.Context(), &vm, "key", `ref.deref() === undefined`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Bool() {
		t.Error("the value kept its WeakMap key alive")
	}
}

// TestFinalizationRegistrySharedToken checks the cleanup of many cells that share an unregister
// token, and that unregistering the token then removes the cells that are left.
func TestFinalizationRegistrySharedToken(t *testing.T) {
	const n = 600000
	var vm ekuma.VM
	if _, err := runScript(t.Context(), &vm, "setup", `
		var token = {}, kept = [], count = 0, sum = 0;
		var r = new FinalizationRegistry(h => { count++; sum += h; });
		for (let i = 0; i < `+strconv.Itoa(n)+`; i++) {
			let o = {};
			if (i % 3 === 0) kept.push(o);
			r.register(o, i, token);
		}
	`); err != nil {
		t.Fatal(err)
	}
	// The cleanup of all the cells finishes well within the timeout.
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	for {
		runtime.GC()
		if _, err := vm.AwaitJobs(ctx); err != nil {
			t.Fatalf("AwaitJobs: %v", err)
		}
		v, err := runScript(ctx, &vm, "count", `count`)
		if err != nil {
			t.Fatal(err)
		}
		if v.Float() == 2*n/3 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := ctx.Err(); err != nil {
		t.Errorf("the cleanup ended after the deadline: %v", err)
	}
	// The sum does not fit in 32 bits.
	var want int64
	for i := range int64(n) {
		if i%3 != 0 {
			want += i
		}
	}
	v, err := runScript(ctx, &vm, "unregister", `[sum, r.unregister(token), r.unregister(token)].join()`)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := v.String(), strconv.FormatInt(want, 10)+",true,false"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestFinalizationRegistryMicrotasksAndThrow(t *testing.T) {
	t.Run("drains promise jobs and reports unhandled rejections", func(t *testing.T) {
		var vm ekuma.VM
		var rejected string
		vm.SetUnhandledRejection(func(promise, reason ekuma.Value) {
			rejected = reason.String()
		})
		if _, err := runScript(t.Context(), &vm, "setup", `
			var cleanupRan = false, microtaskRan = false;
			var reg = new FinalizationRegistry(() => {
				cleanupRan = true;
				Promise.resolve().then(() => { microtaskRan = true; });
				Promise.reject("cleanup-rej");
			});
			reg.register({}, 1);
		`); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			runtime.GC()
			ok, err := vm.AwaitJobs(t.Context())
			if err != nil {
				t.Fatalf("AwaitJobs: %v", err)
			}
			if ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("AwaitJobs: no cleanup became pending")
			}
			time.Sleep(time.Millisecond)
		}
		v, err := runScript(t.Context(), &vm, "check", `[cleanupRan, microtaskRan].join()`)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := v.String(), "true,true"; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
		if rejected != "cleanup-rej" {
			t.Errorf("unhandled rejection: got %q, want %q", rejected, "cleanup-rej")
		}
	})

	t.Run("throwing cleanup does not abort remaining cleanups or fail caller", func(t *testing.T) {
		var vm ekuma.VM
		if _, err := runScript(t.Context(), &vm, "setup", `
			var secondRan = false;
			var r1 = new FinalizationRegistry(() => { throw new Error("cleanup boom"); });
			var r2 = new FinalizationRegistry(() => { secondRan = true; });
			r1.register({}, 1);
			r2.register({}, 2);
		`); err != nil {
			t.Fatal(err)
		}
		s, err := vm.ParseScript("eval", `1 + 1`)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			runtime.GC()
			v, err := s.Evaluate(t.Context())
			if err != nil {
				t.Fatalf("Evaluate failed due to cleanup throw: %v", err)
			}
			if v.Int() != 2 {
				t.Fatalf("got %d, want 2", v.Int())
			}
			done, err := vm.Global().Get("secondRan")
			if err != nil {
				t.Fatal(err)
			}
			if done.Bool() {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("second cleanup did not run")
			}
			time.Sleep(time.Millisecond)
		}
	})
}
