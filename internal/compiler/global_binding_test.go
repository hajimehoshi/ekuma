// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// globalBindingOutcome is what one operation on a global identifier lets a program observe.
type globalBindingOutcome struct {
	// Events lists the callbacks in order.
	Events string

	// Result is the value of the operation, or the name of the exception's constructor.
	Result string

	// Own and Lexical are the own property of the global object and the global lexical binding
	// after the operation.
	Own     string
	Lexical string
}

// globalBindingPrelude defines what the cases share. Every callback moves the interpreter's
// stack, so that the operation has to find its registers again.
const globalBindingPrelude = `
	const events = [];
	const source = {field: 1};
	let rhs = () => 1;
	let grown = 4096;
	function noop() {}
	function event(name) {
		noop(...Array(grown *= 2));
		events.push(name);
	}
	function declareLexical() { $262.evalScript("let binding = 'lexical';"); }
	function define(value) {
		Object.defineProperty(globalThis, "binding", {value, writable: true, configurable: true});
	}
	function declareUninitialized() {
		try { $262.evalScript("throw 0; let binding;"); } catch {}
	}
	function setProxyPrototype(hooks) {
		const target = {binding: 1};
		let lookups = 0;
		Object.setPrototypeOf(globalThis, new Proxy(target, {
			has(t, k) {
				if (k !== "binding") return Reflect.has(t, k);
				event("has");
				return hooks[lookups++]();
			},
			get(t, k, r) {
				if (k === "binding") event("get");
				return Reflect.get(t, k, r);
			},
			set(t, k, v, r) {
				if (k === "binding") event("set");
				return Reflect.set(t, k, v, r);
			},
			deleteProperty(t, k) {
				if (k === "binding") event("delete");
				return Reflect.deleteProperty(t, k);
			}
		}));
	}
	function completion(f) {
		try { return String(f()); } catch (e) { return e.constructor.name; }
	}
	function ownState() {
		const d = Object.getOwnPropertyDescriptor(globalThis, "binding");
		return d ? typeof d.value === "object" ? "object" : String(d.value) : "absent";
	}
	function lexicalState() {
		// A var declaration conflicts with a lexical binding, initialized or not.
		if (completion(() => $262.evalScript("var binding;")) !== "SyntaxError") return "absent";
		const value = completion(() => (0, eval)("binding"));
		return value === "ReferenceError" ? "uninitialized" : value;
	}
`

// checkGlobalBinding runs op, statements that use the global identifier binding, and compares
// each run in a fresh realm with want. The runs differ in the compiler and in whether the call
// site is warm. binding is an own property of the global object until install runs.
func checkGlobalBinding(t *testing.T, install, op string, strict bool, want globalBindingOutcome) {
	t.Helper()
	var directive string
	if strict {
		directive = `"use strict";`
	}
	warmths := []bool{false, true}
	if strings.Contains(op, "delete binding") {
		// A delete has no inline cache.
		warmths = warmths[:1]
	}
	for _, warm := range warmths {
		var warmUp string
		if warm {
			// The last store keeps the shape that the caches remember.
			warmUp = "operate(); operate(); events.length = 0; globalThis.binding = 0;"
		}
		src := fmt.Sprintf(`%s
			function operate() { %s %s }
			globalThis.binding = 0;
			%s
			%s
			const result = completion(operate);
			JSON.stringify({Events: events.join(), Result: result, Own: ownState(), Lexical: lexicalState()});
		`, globalBindingPrelude, directive, op, warmUp, install)
		for _, conservative := range []bool{false, true} {
			var got globalBindingOutcome
			out, err := runAssignment(t.Name(), src, conservative)
			if err == nil {
				err = json.Unmarshal([]byte(out), &got)
			}
			if err != nil || got != want {
				t.Errorf("strict=%t warm=%t conservative=%t: got %+v (%v), want %+v", strict, warm, conservative, got, err, want)
			}
		}
	}
}

func TestGlobalBinding(t *testing.T) {
	const (
		remove = `Reflect.deleteProperty(globalThis, "binding");`
		lazy   = remove + `$262.setLazyPrototype(proto => {
			event("init");
			proto.binding = 1;
			declareLexical();
		});`
		// The lookup reports that the binding does not exist although it has created one.
		missingOwn     = remove + `setProxyPrototype([() => { define(2); return false; }]);`
		missingLexical = remove + `setProxyPrototype([() => { declareLexical(); return false; }]);`
		// The lookup finds the inherited property and removes it.
		vanishing = remove + `setProxyPrototype([() => { Object.setPrototypeOf(globalThis, null); return true; }]);`
		// The lookup finds the inherited property and creates another binding.
		foundOwn           = remove + `setProxyPrototype([() => { define(2); return true; }]);`
		foundUninitialized = remove + `setProxyPrototype([() => { declareUninitialized(); return true; }]);`
		// The second lookup creates the lexical binding, after the operation has selected the property.
		lateLexical = remove + `setProxyPrototype([() => true, () => { declareLexical(); return true; }]);`
		// The assigned value creates the lexical binding after the target is resolved.
		valueLexical = `rhs = () => { event("rhs"); declareLexical(); return 3; };`
	)
	for _, tc := range []struct {
		name    string
		install string
		op      string
		want    globalBindingOutcome

		// strict is the outcome in strict code when it differs from want.
		strict *globalBindingOutcome

		// sloppyOnly marks an operation that strict code cannot express.
		sloppyOnly bool
	}{
		{
			name:    "read/lazy prototype creates a lexical",
			install: lazy,
			op:      `return binding;`,
			want: globalBindingOutcome{
				Events:  "init",
				Result:  "lexical",
				Own:     "absent",
				Lexical: "lexical",
			},
		},
		{
			name:    "read/missing property is created",
			install: missingOwn,
			op:      `return binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "2",
				Lexical: "absent",
			},
		},
		{
			name:    "read/found property is shadowed by an own one",
			install: foundOwn,
			op:      `return binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "2",
				Own:     "2",
				Lexical: "absent",
			},
		},
		{
			name:    "read/found property vanishes",
			install: vanishing,
			op:      `return binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "undefined",
				Own:     "absent",
				Lexical: "absent",
			},
			strict: &globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "absent",
				Lexical: "absent",
			},
		},
		{
			name:    "read/lexical created by the second lookup",
			install: lateLexical,
			op:      `return binding;`,
			want: globalBindingOutcome{
				Events:  "has,has,get",
				Result:  "1",
				Own:     "absent",
				Lexical: "lexical",
			},
		},
		{
			name:    "typeof/lazy prototype creates a lexical",
			install: lazy,
			op:      `return typeof binding;`,
			want: globalBindingOutcome{
				Events:  "init",
				Result:  "string",
				Own:     "absent",
				Lexical: "lexical",
			},
		},
		{
			name:    "typeof/missing property is created",
			install: missingOwn,
			op:      `return typeof binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "undefined",
				Own:     "2",
				Lexical: "absent",
			},
		},
		{
			name:    "typeof/found property vanishes",
			install: vanishing,
			op:      `return typeof binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "undefined",
				Own:     "absent",
				Lexical: "absent",
			},
			strict: &globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "absent",
				Lexical: "absent",
			},
		},
		{
			name:    "typeof/uninitialized lexical is created",
			install: foundUninitialized,
			op:      `return typeof binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "absent",
				Lexical: "uninitialized",
			},
		},
		{
			name:    "assign/lazy prototype creates a lexical",
			install: lazy,
			op:      `return binding = 3;`,
			want: globalBindingOutcome{
				Events:  "init",
				Result:  "3",
				Own:     "absent",
				Lexical: "3",
			},
		},
		{
			name:    "assign/missing property is created",
			install: missingOwn,
			op:      `return binding = 3;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "3",
				Own:     "3",
				Lexical: "absent",
			},
			strict: &globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "2",
				Lexical: "absent",
			},
		},
		{
			name:    "assign/missing lexical is created",
			install: missingLexical,
			op:      `return binding = 3;`,
			want: globalBindingOutcome{
				Events:  "has,set",
				Result:  "3",
				Own:     "3",
				Lexical: "lexical",
			},
			strict: &globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "absent",
				Lexical: "lexical",
			},
		},
		{
			name:    "assign/found property vanishes",
			install: vanishing,
			op:      `return binding = 3;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "3",
				Own:     "3",
				Lexical: "absent",
			},
			strict: &globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "absent",
				Lexical: "absent",
			},
		},
		{
			name:    "assign/lexical created by the second lookup",
			install: lateLexical,
			op:      `return binding = 3;`,
			want: globalBindingOutcome{
				Events:  "has,has,set",
				Result:  "3",
				Own:     "3",
				Lexical: "lexical",
			},
		},
		{
			name:    "assign/value creates a lexical",
			install: valueLexical,
			op:      `return binding = rhs();`,
			want: globalBindingOutcome{
				Events:  "rhs",
				Result:  "3",
				Own:     "0",
				Lexical: "3",
			},
		},
		{
			name:    "destructure/missing property is created",
			install: missingOwn,
			op:      `({field: binding} = source); return "done";`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "done",
				Own:     "1",
				Lexical: "absent",
			},
			strict: &globalBindingOutcome{
				Events:  "has",
				Result:  "ReferenceError",
				Own:     "2",
				Lexical: "absent",
			},
		},
		{
			name:    "update/lazy prototype creates a lexical",
			install: lazy,
			op:      `return binding += "!";`,
			want: globalBindingOutcome{
				Events:  "init",
				Result:  "lexical!",
				Own:     "absent",
				Lexical: "lexical!",
			},
		},
		{
			name: "update/coercion creates a lexical",
			install: `globalThis.binding = {valueOf() {
				event("valueOf");
				declareLexical();
				return 4;
			}};`,
			op: `return binding++;`,
			want: globalBindingOutcome{
				Events:  "valueOf",
				Result:  "4",
				Own:     "object",
				Lexical: "5",
			},
		},
		{
			name:    "delete/lazy prototype creates a lexical",
			install: lazy,
			op:      `return delete binding;`,
			want: globalBindingOutcome{
				Events:  "init",
				Result:  "false",
				Own:     "absent",
				Lexical: "lexical",
			},
			sloppyOnly: true,
		},
		{
			name:    "delete/missing property is created",
			install: missingOwn,
			op:      `return delete binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "true",
				Own:     "2",
				Lexical: "absent",
			},
			sloppyOnly: true,
		},
		{
			name:    "delete/missing lexical is created",
			install: missingLexical,
			op:      `return delete binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "true",
				Own:     "absent",
				Lexical: "lexical",
			},
			sloppyOnly: true,
		},
		{
			name:    "delete/found property is shadowed by an own one",
			install: foundOwn,
			op:      `return delete binding;`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "true",
				Own:     "absent",
				Lexical: "absent",
			},
			sloppyOnly: true,
		},
		{
			name:    "with/value creates a missing lexical",
			install: remove + valueLexical,
			op:      `with (source) { return binding = rhs(); }`,
			want: globalBindingOutcome{
				Events:  "rhs",
				Result:  "3",
				Own:     "3",
				Lexical: "lexical",
			},
			sloppyOnly: true,
		},
		{
			name:    "with/delete of a missing property that is created",
			install: missingOwn,
			op:      `with (source) { return delete binding; }`,
			want: globalBindingOutcome{
				Events:  "has",
				Result:  "true",
				Own:     "2",
				Lexical: "absent",
			},
			sloppyOnly: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkGlobalBinding(t, tc.install, tc.op, false, tc.want)
			if tc.sloppyOnly {
				return
			}
			want := tc.want
			if tc.strict != nil {
				want = *tc.strict
			}
			checkGlobalBinding(t, tc.install, tc.op, true, want)
		})
	}
}
