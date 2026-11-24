// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build ekumavalidate

package engine_test

import (
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/bytecode"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func compareShortcut(t *testing.T, op bytecode.Opcode, setup, scenario, want string, normal, generic []int) {
	t.Helper()
	run := func(bypass bool, path []int) string {
		t.Helper()
		cx := newTestContext(t)
		f, err := runScript(cx, "setup", setup)
		if err != nil {
			t.Fatal(err)
		}
		var steps []int
		engine.TraceShortcut(t, f, op, bypass, &steps)
		v, err := runScript(cx, "scenario", scenario)
		if err != nil {
			t.Fatalf("bypass=%t: %v", bypass, err)
		}
		if !slices.Equal(steps, path) {
			t.Errorf("bypass=%t: next dispatch offsets from %s = %v, want %v", bypass, op, steps, path)
		}
		return string(cx.ToString(v))
	}
	fast := run(false, normal)
	slow := run(true, generic)
	if fast != slow {
		t.Errorf("normal VM observed %s, generic VM observed %s", fast, slow)
	}
	if slow != want {
		t.Errorf("generic VM observed %s, want %s", slow, want)
	}
}

func TestNamedReadDifferential(t *testing.T) {
	const setup = `
		function read(o) { return o.item.value; }
		read;
	`
	const scenario = `
		const a = {item: {value: "a"}}, b = {item: {value: "b"}};
		const values = [read(a), read(b)];
		let calls = "";
		Object.defineProperty(b, "item", {get() {
			calls += this === b ? "b" : "?";
			return {value: ++this.count};
		}});
		b.count = 0;
		values.push(read(b), read(b));
		JSON.stringify([values, calls, b.count]);
	`
	// On the same-shape receiver b, the warm chain skips its second instruction.
	// A cold cache or an accessor dispatches the second read separately.
	compareShortcut(t, bytecode.OpGetPropChain, setup, scenario, `[["a","b",1,2],"bb",2]`,
		[]int{1, 2, 1, 1}, []int{1, 1, 1, 1})
}

func TestGlobalDestructuringDifferential(t *testing.T) {
	const setup = `
		globalThis.target = 0;
		function assign(o) { "use strict"; ({field: target} = o); }
		assign;
	`
	const scenario = `
		const events = [];
		const a = {field: 1}, b = {field: 2};
		assign(a);
		assign(b);
		events.push(target);

		const child = Object.create(a);
		assign(child);
		a.field = 3;
		assign(child);
		events.push(target);
		Object.defineProperty(a, "field", {get() {
			events.push(this === child ? "get" : "wrong receiver");
			return 4;
		}});
		assign(child);
		assign(child);
		events.push(target);

		assign(b);
		Object.defineProperty(globalThis, "target", {set(v) { events.push(v); }});
		assign(b);
		JSON.stringify(events);
	`
	// Cached data skips the full assignment (+6). A cached getter dispatches
	// the read (+2), and a cache miss continues at the read (+1).
	compareShortcut(t, bytecode.OpCopyPropToGlobal, setup, scenario, `[2,3,"get","get",4,2]`,
		[]int{1, 6, 1, 6, 1, 2, 1, 1}, []int{1, 1, 1, 1, 1, 1, 1, 1})
}

func TestGeneratorMethodDifferential(t *testing.T) {
	const setup = `
		function step(it) { return it.next(); }
		step;
	`
	const scenario = `
		let events = "";
		const token = {};
		const it = (function* () {
			try { yield 1; yield 2; }
			finally { events += "finally "; yield 3; events += "resume "; }
		})();
		const next = it.next;
		const first = step(it), second = step(it);
		Object.defineProperty(it, "next", {get() {
			events += "get ";
			const r = this.return(token);
			events += r.value + " " + r.done + " ";
			return next;
		}});
		const end = step(it);
		JSON.stringify([first.value, second.value, events, end.value === token, end.done]);
	`
	// The warm lookup consumes the call (+2). The getter dispatches it separately (+1).
	compareShortcut(t, bytecode.OpGetMethod, setup, scenario,
		`[1,2,"get finally 3 false resume ",true,true]`, []int{1, 2, 1}, []int{1, 1, 1})
}

func TestIteratorPairDifferential(t *testing.T) {
	const setup = `
		const marker = {};
		let events = "";
		function bind(it) {
			let first = "unset", second = "unset", caught = false;
			try {
				for ([first, second] of it) { events += "body "; break; }
			} catch (e) {
				caught = e === marker;
			}
			return [first, second, caught];
		}
		bind;
	`
	const scenario = `
		const original = Array.prototype[Symbol.iterator];
		const it = Array.prototype.entries.call({
			length: 2,
			get 0() {
				events += "get ";
				Array.prototype[Symbol.iterator] = function* () {
					events += "yield ";
					yield this[1];
					events += "throw ";
					throw marker;
				};
				return "entry";
			},
			1: "tail"
		});
		it.return = function () { events += "close "; return {}; };
		let binding;
		try { binding = bind(it); }
		finally { Array.prototype[Symbol.iterator] = original; }
		const tail = it.next();
		JSON.stringify([binding, events, tail.value, tail.done, it.next().done]);
	`
	// The pair occupies two words. A step that already yielded resumes at the
	// binding (+3), while the generic path resumes at the outer OpIterNext (+2).
	compareShortcut(t, bytecode.OpIterNextPair, setup, scenario,
		`[["entry","unset",true],"get yield throw close ",[1,"tail"],false,true]`, []int{3}, []int{2})
}

func TestValidationRejectsSkippedLoad(t *testing.T) {
	f := &bytecode.Func{
		NumRegs: 1,
		Code: []bytecode.Instr{
			{
				Op: bytecode.OpLoadUndef,
			},
			{
				Op: bytecode.OpLoadTrue,
			},
			{
				Op: bytecode.OpReturn,
			},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("validation accepted a dispatch that skipped the second load")
		}
	}()
	engine.CheckDispatchSequence(f, 0, 2)
}

func TestValidationRejectsWrongCacheEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "own read",
			src:  `function f(o) { return o.b; } f;`,
		},
		{
			name: "inherited read",
			src:  `function f(o) { return o.c; } f;`,
		},
		{
			name: "missing read",
			src:  `function f(o) { return o.missing; } f;`,
		},
		{
			name: "getter",
			src:  `function f(o) { return o.d; } f;`,
		},
		{
			name: "store",
			src:  `function f(o) { o.b = 3; } f;`,
		},
		{
			name: "add",
			src:  `function f(o) { o.e = 3; } f;`,
		},
		{
			name: "setter",
			src:  `function f(o) { o.d = 3; } f;`,
		},
		{
			name: "global read",
			src:  `function f() { return value; } f;`,
		},
		{
			name: "global store",
			src:  `function f() { value = 2; } f;`,
		},
		{
			name: "method",
			src:  `function f(o) { return o.m(); } f;`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cx := newTestContext(t)
			if _, err := runScript(cx, "setup", `
				var value = 1;
				const proto = {c: 1, get d() {}, set d(v) {}, m() {}};
				const make = () => Object.assign(Object.create(proto), {a: 1, b: 2});
			`); err != nil {
				t.Fatal(err)
			}
			f, err := runScript(cx, "function", tc.src)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := runScript(cx, "warm", `f(make());`); err != nil {
					t.Fatal(err)
				}
			}
			engine.CorruptCaches(f)
			defer func() {
				// The checks panic with a string, and a failed array access with an error.
				if _, ok := recover().(string); !ok {
					t.Error("validation accepted the wrong entry")
				}
			}()
			if _, err := runScript(cx, "hit", `f(make());`); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestValidationRejectsStaleRegisters tests that a frame that misses reloading its registers
// after a call fails, though the stack has room and would not grow.
func TestValidationRejectsStaleRegisters(t *testing.T) {
	f := &bytecode.Func{
		NumRegs: 2,
		Code: []bytecode.Instr{
			{
				Op: bytecode.OpLoadUndef,
			},
			{
				Op: bytecode.OpMove,
				B:  1,
			},
			{
				Op: bytecode.OpReturn,
			},
		},
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		use  func(r *engine.Registers)
		want bool
	}{
		{
			name: "reload",
			use: func(r *engine.Registers) {
				r.EnsureStack()
				r.Reload()
				r.Set(0, r.Get(1))
			},
			want: true,
		},
		{
			name: "no reload",
			use: func(r *engine.Registers) {
				r.EnsureStack()
			},
		},
		{
			name: "read before the reload",
			use: func(r *engine.Registers) {
				r.EnsureStack()
				v := r.Get(1)
				r.Reload()
				r.Set(0, v)
			},
		},
		{
			name: "write before the reload",
			use: func(r *engine.Registers) {
				r.EnsureStack()
				r.Set(0, engine.True)
				r.Reload()
			},
		},
		{
			name: "write after a reload and two more moves",
			use: func(r *engine.Registers) {
				r.EnsureStack()
				r.Reload()
				r.EnsureStack()
				r.EnsureStack()
				r.Set(0, engine.True)
				r.Reload()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := engine.NewRegisters(f)
			r.Step(0)
			r.Step(1)
			defer func() {
				p := recover()
				if accepted := p == nil; accepted != tt.want {
					t.Errorf("accepted = %t, want %t (panic: %v)", accepted, tt.want, p)
				}
			}()
			tt.use(r)
			r.Step(2)
		})
	}
}
