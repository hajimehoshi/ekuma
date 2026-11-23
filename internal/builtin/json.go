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

func initJSON(cx *engine.ExecutionContext) {
	st := &jsonState{}

	j := cx.NewOrdinaryObject()
	cx.Define(cx.GlobalObject().Object(), cx.Intern("JSON"), engine.ObjectValue(j), engine.FlagsHidden)
	cx.Define(j, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("JSON"), engine.FlagConfigurable)

	parse := method(cx, j, "parse", 2, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		text := cx.ToString(arg(args, 0))
		reviver := arg(args, 1)
		if !reviver.IsCallable() {
			return st.parser.Parse(cx, text)
		}
		v, rec := st.parser.ParseWithRecord(cx, text)
		return json.Revive(cx, v, rec, reviver.Object())
	})
	cx.SetIntrinsic(engine.IntrinsicJSONParse, parse)

	method(cx, j, "stringify", 3, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return st.stringifier.Stringify(cx, arg(args, 0), arg(args, 1), arg(args, 2))
	})

	method(cx, j, "rawJSON", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return json.RawJSON(cx, arg(args, 0))
	})

	method(cx, j, "isRawJSON", 1, func(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
		return engine.Bool(json.IsRawJSON(arg(args, 0)))
	})
}
