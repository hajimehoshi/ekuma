// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewSymbol returns a new symbol. desc is its description when hasDesc is set.
func (sp *space) NewSymbol(desc wtf8.String, hasDesc bool) *Object {
	s := &Object{
		kind:  kindSymbol,
		shape: sp.symbolShape,
		str:   desc,
	}
	if hasDesc {
		s.setExtState(true)
	}
	return s
}

// newPrivateName returns a new private name whose description is desc.
func newPrivateName(desc wtf8.String) *Object {
	return &Object{
		kind:  kindSymbol,
		class: classPrivateName,
		str:   desc,
	}
}

// RegisteredSymbol returns the symbol of the GlobalSymbolRegistry List with the key key,
// which it adds when there is none.
func (sp *space) RegisteredSymbol(key wtf8.String) *Object {
	if s, ok := sp.symbolRegistry[key]; ok {
		return s
	}
	s := sp.NewSymbol(key, true)
	sp.symbolRegistry[key] = s
	return s
}

// KeyForSymbol implements KeyForSymbol: it returns the key of the symbol sym in the
// GlobalSymbolRegistry List, and reports whether sym is in it.
func (sp *space) KeyForSymbol(sym *Object) (wtf8.String, bool) {
	if r, ok := sp.symbolRegistry[sym.str]; !ok || r != sym {
		return "", false
	}
	return sym.str, true
}
