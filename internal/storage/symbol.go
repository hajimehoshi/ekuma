// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// NewSymbol returns a new symbol. desc is its description when hasDesc is set.
func (sp *Space) NewSymbol(desc wtf8.String, hasDesc bool) *Object {
	if hasDesc {
		return (&textTailObject{
			Kind:  KindSymbol,
			shape: sp.SymbolShape,
			text:  desc,
			tail:  objectTail{Ext: true},
		}).Init()
	}
	return newText(&textObject{
		Kind:       KindSymbol,
		shape:      sp.SymbolShape,
		text:       desc,
		objectTail: &zeroTail,
	})
}

// NewPrivateName returns a new private name whose description is desc.
func NewPrivateName(desc wtf8.String) *Object {
	return newText(&textObject{
		Kind:       KindSymbol,
		Class:      ClassPrivateName,
		text:       desc,
		objectTail: &zeroTail,
	})
}

// RegisteredSymbol returns the symbol of the GlobalSymbolRegistry List with the key key,
// which it adds when there is none.
func (sp *Space) RegisteredSymbol(key wtf8.String) *Object {
	if s, ok := sp.SymbolRegistry[key]; ok {
		return s
	}
	s := sp.NewSymbol(key, true)
	sp.SymbolRegistry[key] = s
	return s
}

// KeyForSymbol implements KeyForSymbol: it returns the key of the symbol sym in the
// GlobalSymbolRegistry List, and reports whether sym is in it.
func (sp *Space) KeyForSymbol(sym *Object) (wtf8.String, bool) {
	if r, ok := sp.SymbolRegistry[sym.Text()]; !ok || r != sym {
		return "", false
	}
	return sym.Text(), true
}

// SymbolDescription returns the description of the symbol sym and whether it has one.
func SymbolDescription(sym *Object) (wtf8.String, bool) {
	return sym.Text(), sym.ExtState() != nil
}
