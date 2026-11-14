// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/json"
)

// jsonState is the state that the JSON functions of one VM share.
type jsonState struct {
	parser      json.Parser
	stringifier json.Stringifier
}

func initJSON(vm *engine.VM) {
	st := &jsonState{}

	j := vm.NewOrdinaryObject()
	vm.Define(vm.GlobalObject().Object(), vm.Intern("JSON"), engine.ObjectValue(j), engine.FlagsHidden)
	vm.Define(j, vm.WellKnownSymbol(engine.SymbolToStringTag), vm.NewString("JSON"), engine.FlagConfigurable)

	method(vm, j, "parse", 2, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		text := vm.ToString(arg(args, 0))
		reviver := arg(args, 1)
		if !reviver.IsCallable() {
			return st.parser.Parse(vm, text)
		}
		v, rec := st.parser.ParseWithRecord(vm, text)
		return json.Revive(vm, v, rec, reviver.Object())
	})

	method(vm, j, "stringify", 3, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return st.stringifier.Stringify(vm, arg(args, 0), arg(args, 1), arg(args, 2))
	})

	method(vm, j, "rawJSON", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return json.RawJSON(vm, arg(args, 0))
	})

	method(vm, j, "isRawJSON", 1, func(vm *engine.VM, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(json.IsRawJSON(arg(args, 0)))
	})
}
