// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package json

import (
	"strconv"

	"github.com/hajimehoshi/ekuma/internal/engine"
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// RawJSON implements JSON.rawJSON.
func RawJSON(cx *engine.ExecutionContext, text engine.Value) engine.Value {
	s := cx.ToStringValue(text)
	str := s.Str()
	if str == "" || IsSpace(str[0]) || IsSpace(str[len(str)-1]) {
		cx.ThrowSyntaxError("JSON.rawJSON: the text must not be empty or start or end with whitespace")
	}
	if str[0] == '{' || str[0] == '[' {
		cx.ThrowSyntaxError("JSON.rawJSON: the text must be a primitive value")
	}
	if pos, ok := Valid(str, cx.Poll); !ok {
		cx.ThrowSyntaxError("JSON.rawJSON: unexpected character in JSON at position " + strconv.Itoa(wtf8.Len(str[:pos])))
	}
	// The object is frozen: its only property is read-only and it accepts no others.
	o := cx.NewRawJSONObject()
	cx.Define(o, cx.Intern("rawJSON"), s, engine.FlagEnumerable)
	cx.PreventExtensions(o)
	return storage.ObjectValue(o)
}

// IsRawJSON implements JSON.isRawJSON.
func IsRawJSON(v engine.Value) bool {
	return v.IsObject() && v.Object().IsRawJSON()
}
