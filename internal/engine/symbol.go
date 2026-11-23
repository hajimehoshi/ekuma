// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewSymbol returns a new symbol. desc is its description when hasDesc is set.
func (ag *agent) NewSymbol(desc wtf8.String, hasDesc bool) *Object {
	s := &Object{
		kind:  kindSymbol,
		shape: ag.symbolShape,
		str:   desc,
	}
	if hasDesc {
		s.setExtState(true)
	}
	return s
}

// RegisteredSymbol returns the symbol of the GlobalSymbolRegistry List with the key key,
// which it adds when there is none.
func (ag *agent) RegisteredSymbol(key wtf8.String) *Object {
	if s, ok := ag.symbolRegistry[key]; ok {
		return s
	}
	s := ag.NewSymbol(key, true)
	ag.symbolRegistry[key] = s
	return s
}

// KeyForSymbol implements KeyForSymbol: it returns the key of the symbol sym in the
// GlobalSymbolRegistry List, and reports whether sym is in it.
func (ag *agent) KeyForSymbol(sym *Object) (wtf8.String, bool) {
	if r, ok := ag.symbolRegistry[sym.str]; !ok || r != sym {
		return "", false
	}
	return sym.str, true
}
