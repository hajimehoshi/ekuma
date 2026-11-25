// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package compiler_test

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/compiler"
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/parser"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func runAssignment(file, src string, conservative bool) (string, error) {
	cx, _ := engine.NewExecutionContext(nil)
	builtin.Install(cx)
	compile := func(file string, source wtf8.String) (*engine.Script, error) {
		if !conservative {
			return cx.ParseScript(file, source)
		}
		p, err := parser.ParseScript(source, parser.Options{})
		if err != nil {
			return nil, err
		}
		code, err := compiler.CompileConservativeScript(p, file)
		if err != nil {
			return nil, err
		}
		return cx.NewScript(code), nil
	}
	eval := cx.NewFunction("evalScript", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		var input wtf8.String
		if len(args) > 0 {
			input = cx.ToString(args[0])
		}
		s, err := compile("evalScript", input)
		if err != nil {
			cx.ThrowSyntaxError(err.Error())
		}
		return s.CallScript()
	})
	lazy := cx.NewFunction("setLazyPrototype", func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		init := args[0].Object()
		global := cx.GlobalObject().Object()
		proto := cx.NewLazyObject(cx.ProtoOf(global), func(cx *engine.ExecutionContext, o *engine.Object) {
			cx.Call(init, storage.Undefined, storage.ObjectValue(o))
		})
		return storage.Bool(cx.SetPrototypeOf(global, proto))
	})
	host := storage.ObjectValue(cx.NewOrdinaryObject())
	if err := cx.HostSet(host, cx.NewString("evalScript"), eval); err != nil {
		return "", err
	}
	if err := cx.HostSet(host, cx.NewString("setLazyPrototype"), lazy); err != nil {
		return "", err
	}
	if err := cx.HostSet(cx.GlobalObject(), cx.NewString("$262"), host); err != nil {
		return "", err
	}
	for _, name := range []string{"sta", "assert"} {
		file := "../jstest/testdata/harness/test262-" + name + ".js"
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		s, err := cx.ParseScript(file, wtf8.FromValidUTF8(string(b)))
		if err != nil {
			return "", err
		}
		if _, _, err := s.ScriptEvaluation(); err != nil {
			return "", err
		}
	}
	s, err := compile(file, wtf8.FromValidUTF8(src))
	if err != nil {
		return "", err
	}
	v, _, err := s.ScriptEvaluation()
	if err != nil {
		return "", err
	}
	return string(cx.ToString(v)), nil
}

// compareAssignment runs src in fresh realms and checks each completion against want.
// Fixtures can also use assert.sameValue and $262.evalScript in the same realm.
func compareAssignment(t *testing.T, src, want string) {
	t.Helper()
	normal, normalErr := runAssignment(t.Name(), src, false)
	conservative, conservativeErr := runAssignment(t.Name(), src, true)
	if normalErr != nil || conservativeErr != nil || normal != want || conservative != want {
		t.Errorf("normal: %q (%v)\nconservative: %q (%v)\nwant: %q\nsource:\n%s",
			normal, normalErr, conservative, conservativeErr, want, src)
	}
}

func TestAssignmentOrderFixtures(t *testing.T) {
	for _, path := range []string{
		"assignment/strict-global-reference-before-coercion",
		"captured-variables/operand-before-unary-coercion",
		"captured-variables/operand-before-global-getter",
		"assignment/destructuring-source-assigned-to-target",
		"assignment/computed-read-after-key-coercion",
		"assignment/destructuring-resolves-global-before-getter",
		"assignment/unresolvable-name-before-global-lexical",
		"proxy/global-assignment-around-getter",
		"assignment/member-reference-before-getter",
		"assignment/throwing-setter-preserves-destination",
		"destructuring/bindings-written-once-by-each-step",
	} {
		b, err := os.ReadFile("../../testdata/scripts/" + path + ".js")
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, strict := range []bool{false, true} {
			if strict && strings.Contains(src, "flags: [noStrict") || !strict && strings.Contains(src, "flags: [onlyStrict") {
				continue
			}
			t.Run(fmt.Sprintf("%s/strict=%t", path, strict), func(t *testing.T) {
				program := src
				if strict {
					program = "\"use strict\";\n" + program
				}
				compareAssignment(t, program, "undefined")
			})
		}
	}
}

func TestAssignmentOrder(t *testing.T) {
	for _, tc := range []struct {
		name       string
		src        string
		want       string
		wantStrict string
	}{
		{
			name: "target created during coercion",
			src: `
				let events = [];
				function assign(v) { late = -v; }
				globalThis.late = 0;
				assign(1); assign(2);
				delete globalThis.late;
				let v = {valueOf() {
					events.push(this === v);
					globalThis.late = 12;
					return 3;
				}};
				try { assign(v); } catch (e) { events.push(e instanceof ReferenceError); }
				events.push(globalThis.late);
				events.join();
			`,
			want:       "true,-3",
			wantStrict: "true,true,12",
		},
		{
			name: "global lookup creates lexical before update",
			src: `
				const events = [];
				function step() { "use strict"; return counter++; }
				globalThis.counter = 1;
				step(); step(); delete globalThis.counter;
				Object.setPrototypeOf(globalThis, new Proxy({counter: 10}, {
					has(o, k) {
						if (k === "counter") {
							events.push("has");
							if (events.length === 1) $262.evalScript("let counter = 20;");
						}
						return Reflect.has(o, k);
					},
					get(o, k, r) { events.push("get"); return Reflect.get(o, k, r); }
				}));
				events.push(step(), counter);
				events.join();
			`,
			want: "has,20,21",
		},
		{
			name: "global lookup removes its prototype",
			src: `
				const events = [];
				function assign() { "use strict"; volatile ||= (events.push("rhs"), 3); }
				globalThis.volatile = 1;
				assign(); assign(); delete globalThis.volatile;
				Object.setPrototypeOf(globalThis, new Proxy({}, {
					has(o, k) {
						if (k !== "volatile") return Reflect.has(o, k);
						events.push("has");
						Object.setPrototypeOf(globalThis, null);
						return true;
					}
				}));
				try { assign(); } catch (e) { events.push(e instanceof ReferenceError); }
				events.push(Object.hasOwn(globalThis, "volatile"));
				events.join();
			`,
			want: "has,true,false",
		},
		{
			name: "prototype getter changes destination",
			src: `
				let events = [];
				const proto = {field: 1}, child = Object.create(proto);
				globalThis.target = 0;
				function copy() { "use strict"; ({field: target} = child); }
				copy(); copy();
				Object.defineProperty(proto, "field", {get() {
					events.push(this === child);
					Object.defineProperty(globalThis, "target", {set(v) {
						events.push(this === globalThis, v);
					}, configurable: true});
					return 4;
				}});
				copy(); copy();
				events.push(target);
				JSON.stringify(events);
			`,
			want: "[true,true,4,true,true,4,null]",
		},
		{
			name: "compound value across coercion",
			src: `
				const events = [];
				const proto = {value: 1}, o = Object.create(proto);
				function add(right, change) {
					if (change) {
						const left = {valueOf() {
							events.push(this === left ? "convert" : "receiver");
							right = 99;
							Object.setPrototypeOf(o, {set value(v) { events.push(this === o, v); }});
							return 10;
						}};
						Object.defineProperty(proto, "value", {get() {
							events.push(this === o ? "get" : "receiver");
							return left;
						}});
					}
					return [o.value += right, right];
				}
				add(2); add(3); delete o.value;
				events.push(...add(5, true), Object.hasOwn(o, "value"));
				events.join();
			`,
			want: "get,convert,true,15,15,99,false",
		},
		{
			name: "iterator value throws before store",
			src: `
				const marker = {}, events = [];
				function bind(it) {
					let first = "old", second = "old";
					try { [first, second] = it; } catch (e) { events.push(e === marker); }
					return [first, second];
				}
				bind([1, 2]); bind([3, 4]);
				let count = 0;
				const it = {
					[Symbol.iterator]() { events.push(this === it); return this; },
					next() {
						events.push(this === it);
						if (++count === 1) return {value: 5};
						return {get value() { events.push("value"); throw marker; }};
					},
					return() { events.push("close"); return {}; }
				};
				events.push(...bind(it));
				events.join();
			`,
			want: "true,true,true,value,true,5,old",
		},
		{
			name: "with logical short circuit",
			src: `
				const events = [], object = {x: 1};
				const scope = new Proxy(object, {
					has(o, k) { if (k === "x") events.push("has"); return Reflect.has(o, k); },
					get(o, k, r) {
						if (k === Symbol.unscopables) events.push("unscopables");
						if (k === "x") events.push(r === scope ? "get" : "receiver");
						return Reflect.get(o, k, r);
					},
					set(o, k, v, r) { events.push(r === scope ? "set" : "receiver"); return Reflect.set(o, k, v, r); }
				});
				function rhs() { events.push("rhs"); return 3; }
				function assign() { with (scope) { return x ||= rhs(); } }
				assign(); assign(); events.length = 0;
				events.push(assign());
				object.x = 0;
				events.push(assign(), object.x);
				events.join();
			`,
			want: "has,unscopables,has,get,1,has,unscopables,has,get,rhs,has,set,3,3",
		},
		{
			name: "computed target across default",
			src: `
				const events = [];
				function bind(src, dest, key) {
					({[key("source")]: dest[key("target")] = (events.push("default"), 7)} = src);
				}
				const dest = {}, plain = k => k;
				bind({source: 1}, dest, plain); bind({source: 2}, dest, plain);
				const src = {get source() { events.push(this === src ? "get" : "receiver"); }};
				bind(src, dest, k => {
					events.push("expr " + k);
					return {toString() { events.push("key " + k); return k; }};
				});
				events.push(dest.target);
				events.join();
			`,
			want: "expr source,key source,expr target,get,default,key target,7",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compareAssignment(t, tc.src, tc.want)
			if tc.wantStrict != "" {
				compareAssignment(t, "\"use strict\";\n"+tc.src, tc.wantStrict)
			}
		})
	}
}
