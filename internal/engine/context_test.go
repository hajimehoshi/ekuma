// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine_test

import (
	"errors"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/builtin"
	"github.com/hajimehoshi/ekuma/internal/engine"
)

func TestExecutionContextHostReentry(t *testing.T) {
	a, host := engine.NewExecutionContext(nil)
	builtin.Install(a)
	b := a.CreateRealm(builtin.Install)
	c := a.CreateRealm(builtin.Install)
	stop := errors.New("stop")

	for _, outcome := range []string{"return", "allocate", "throw", "iterator step", "interrupt"} {
		t.Run(outcome, func(t *testing.T) {
			var outer *engine.ExecutionContext
			var inner engine.Value
			c.Run(func() {
				probe := c.NewFunction("probe", func(cx *engine.ExecutionContext, _ engine.Value, args []engine.Value) engine.Value {
					if host.ActiveContext() != c || !a.IsEnteredRealm() {
						t.Error("expected running C and entered A during reentry")
					}
					switch outcome {
					case "allocate":
						return outer.NewArrayOf(nil)
					case "throw":
						outer.ThrowTypeError("from B")
					case "iterator step":
						defer func() {
							if host.ActiveContext() != c {
								t.Error("iterator step did not restore C on throw")
							}
						}()
						outer.IteratorStepValue(engine.Undefined, args[0])
					case "interrupt":
						cx.Interrupt(stop)
						cx.Poll()
					}
					return engine.Undefined
				})
				c.Define(c.GlobalObject().Object(), c.Intern("probe"), probe, engine.FlagsData)
				var err error
				inner, err = runScript(c, "C", "(function () { return probe(() => 0) ?? []; })")
				if err != nil {
					t.Fatal(err)
				}
			})

			var ctor *engine.Object
			b.Run(func() {
				ctor = b.NewNativeConstructor("Bridge", 0, func(cx *engine.ExecutionContext, _ engine.Value, _ []engine.Value) engine.Value {
					outer = cx
					defer func() {
						if host.ActiveContext() != b || !a.IsEnteredRealm() {
							t.Error("return from C did not restore running B and entered A")
						}
					}()
					v, err := cx.HostCall(inner, engine.Undefined, nil)
					if ex, ok := errors.AsType[*engine.Error](err); ok {
						cx.Throw(ex.Value)
					}
					if err != nil {
						t.Fatalf("unexpected error from C: %v", err)
					}
					return v
				})
			})
			v, err := a.HostConstruct(engine.ObjectValue(ctor), nil)
			switch outcome {
			case "return", "allocate":
				if err != nil {
					t.Fatal(err)
				}
				want := c
				if outcome == "allocate" {
					want = b
				}
				if a.ProtoOf(v.Object()) != want.Intrinsic(engine.IntrinsicArrayPrototype) {
					t.Error("array has the wrong prototype")
				}
			case "throw", "iterator step":
				ex, ok := errors.AsType[*engine.Error](err)
				if !ok {
					t.Fatalf("got %v, want a JavaScript throw", err)
				}
				if a.ProtoOf(ex.Value.Object()) != b.Intrinsic(engine.IntrinsicTypeErrorPrototype) {
					t.Error("B's context threw an error with the wrong prototype while C was running")
				}
			case "interrupt":
				if !errors.Is(err, stop) {
					t.Errorf("got %v, want an interruption carrying %v", err, stop)
				}
			}
			if host.ActiveContext() != a || a.IsEnteredRealm() {
				t.Error("outer host boundary did not restore A and clear the entered realm")
			}
		})
	}
	v, err := runScript(a, "after interruption", "[]")
	if err != nil {
		t.Fatal(err)
	}
	if a.ProtoOf(v.Object()) != a.Intrinsic(engine.IntrinsicArrayPrototype) {
		t.Error("script after interruption produced an array outside A")
	}
}

func TestSameRealmContextCallAllocations(t *testing.T) {
	if engine.ValidateExec {
		t.Skip("the checks of the interpreter allocate")
	}
	cx, _ := engine.NewExecutionContext(nil)
	f := cx.NewNative("noop", 0, func(*engine.ExecutionContext, engine.Value, []engine.Value) engine.Value {
		return engine.Undefined
	})
	if got := testing.AllocsPerRun(100, func() { cx.Call(f, engine.Undefined) }); got != 0 {
		t.Errorf("same-realm native call allocated %v times, want 0", got)
	}
}
