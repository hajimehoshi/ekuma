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
	vm := newVM(t)
	a := vm.CurrentRealm()
	b := vm.CreateRealm(func() { builtin.Install(vm) })
	c := vm.CreateRealm(func() { builtin.Install(vm) })
	stop := errors.New("stop")

	for _, outcome := range []string{"return", "throw", "interrupt"} {
		t.Run(outcome, func(t *testing.T) {
			var outer engine.ExecutionContext
			var inner engine.Value
			vm.InRealm(c, func() {
				probe := vm.NewFunction("probe", func(vm *engine.VM, _ engine.Value, _ []engine.Value) engine.Value {
					if vm.CurrentRealm() != c || !vm.IsEnteredRealm(a) {
						t.Error("expected running C and entered A during reentry")
					}
					switch outcome {
					case "throw":
						outer.ThrowTypeError("from B")
					case "interrupt":
						vm.Interrupt(stop)
						vm.Poll()
					}
					return engine.Undefined
				})
				vm.Define(vm.GlobalObject().Object(), vm.Intern("probe"), probe, engine.FlagsData)
				var err error
				inner, err = runScript(vm, "C", "(function () { probe(); return []; })")
				if err != nil {
					t.Fatal(err)
				}
			})

			var ctor *engine.Object
			vm.InRealm(b, func() {
				ctor = vm.NewNativeConstructor("Bridge", 0, engine.WithExecutionContext(func(cx engine.ExecutionContext, _ engine.Value, _ []engine.Value) engine.Value {
					outer = cx
					defer func() {
						if vm.CurrentRealm() != b || !vm.IsEnteredRealm(a) {
							t.Error("return from C did not restore running B and entered A")
						}
					}()
					v, err := vm.HostCall(inner, engine.Undefined, nil)
					if ex, ok := errors.AsType[*engine.Error](err); ok {
						vm.Throw(ex.Value)
					}
					if err != nil {
						t.Fatalf("unexpected error from C: %v", err)
					}
					return v
				}))
			})
			v, err := vm.HostConstruct(engine.ObjectValue(ctor), nil)
			switch outcome {
			case "return":
				if err != nil {
					t.Fatal(err)
				}
				if vm.ProtoOf(v.Object()) != c.Intrinsic(engine.IntrinsicArrayPrototype) {
					t.Error("array returned by C has the wrong prototype")
				}
			case "throw":
				ex, ok := errors.AsType[*engine.Error](err)
				if !ok {
					t.Fatalf("got %v, want a JavaScript throw", err)
				}
				if vm.ProtoOf(ex.Value.Object()) != b.Intrinsic(engine.IntrinsicTypeErrorPrototype) {
					t.Error("B's context threw an error with the wrong prototype while C was running")
				}
			case "interrupt":
				if !errors.Is(err, stop) {
					t.Errorf("got %v, want an interruption carrying %v", err, stop)
				}
			}
			if vm.CurrentRealm() != a || vm.IsEnteredRealm(a) {
				t.Error("outer host boundary did not restore A and clear the entered realm")
			}
		})
	}
	v, err := runScript(vm, "after interruption", "[]")
	if err != nil {
		t.Fatal(err)
	}
	if vm.ProtoOf(v.Object()) != a.Intrinsic(engine.IntrinsicArrayPrototype) {
		t.Error("script after interruption produced an array outside A")
	}
}

func TestSameRealmContextCallAllocations(t *testing.T) {
	vm := engine.NewVM(nil)
	f := vm.NewNative("noop", 0, engine.WithExecutionContext(func(engine.ExecutionContext, engine.Value, []engine.Value) engine.Value {
		return engine.Undefined
	}))
	if got := testing.AllocsPerRun(100, func() { vm.Call(f, engine.Undefined) }); got != 0 {
		t.Errorf("same-realm native call allocated %v times, want 0", got)
	}
}
