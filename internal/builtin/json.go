// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package builtin

import (
	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/json"
	"github.com/hajimehoshi/ekuma/internal/storage"
)

var (
	nativeJsonParse     = engine.RegisterNative(jsonParse)
	nativeJsonStringify = engine.RegisterNative(jsonStringify)
	nativeJsonRawJSON   = engine.RegisterNative(jsonRawJSON)
	nativeJsonIsRawJSON = engine.RegisterNative(jsonIsRawJSON)
)

func initJSON(cx *engine.ExecutionContext) {
	j := cx.NewOrdinaryObject()
	cx.Define(cx.GlobalObject().Object(), cx.Intern("JSON"), storage.ObjectValue(j), engine.FlagsHidden)
	cx.Define(j, cx.WellKnownSymbol(engine.SymbolToStringTag), cx.SharedString("JSON"), engine.FlagConfigurable)

	parse := methodWithState(cx, j, "parse", 2, nativeJsonParse, &json.Parser{})
	cx.SetIntrinsic(engine.IntrinsicJSONParse, parse)

	methodWithState(cx, j, "stringify", 3, nativeJsonStringify, &json.Stringifier{})

	method(cx, j, "rawJSON", 1, nativeJsonRawJSON)

	method(cx, j, "isRawJSON", 1, nativeJsonIsRawJSON)
}

func jsonParse(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	text := cx.ToString(arg(args, 0))
	reviver := arg(args, 1)
	parser := cx.ActiveNativeState().(*json.Parser)
	if !reviver.IsCallable() {
		return parser.Parse(cx, text)
	}
	v, rec := parser.ParseWithRecord(cx, text)
	return json.Revive(cx, v, rec, reviver.Object())
}

func jsonStringify(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return cx.ActiveNativeState().(*json.Stringifier).Stringify(cx, arg(args, 0), arg(args, 1), arg(args, 2))
}

func jsonRawJSON(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return json.RawJSON(cx, arg(args, 0))
}

func jsonIsRawJSON(cx *engine.ExecutionContext, this engine.Value, args []engine.Value) engine.Value {
	return storage.Bool(json.IsRawJSON(arg(args, 0)))
}
