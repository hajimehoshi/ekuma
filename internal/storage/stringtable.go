// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// stringTable holds the strings that are shared.
type stringTable struct {
	// Interned maps a string to its property key.
	Interned sweptTable[wtf8.String, Object]

	// keepNew is set while the agent and its built-in objects are created. The strings interned
	// meanwhile are kept alive with the agent.
	keepNew bool

	// Ascii are the strings of one ASCII character.
	Ascii [128]Value

	// Empty is the empty string.
	Empty Value
}

// Init creates the empty string and the strings of one ASCII character with the shape
// stringShape. The table must be keeping new strings alive.
func (t *stringTable) Init(stringShape *Shape) {
	t.Interned.Entries = make(map[wtf8.String]sweptRef[Object], internedInitialSize)
	t.Empty = ObjectValue(t.add("", stringShape))
	for i := range t.Ascii {
		t.Ascii[i] = ObjectValue(t.add(wtf8.String(rune(i)), stringShape))
	}
}

// StartKeeping starts keeping newly interned strings alive with the agent, which EndKeeping ends.
func (t *stringTable) StartKeeping() {
	t.keepNew = true
}

// EndKeeping ends what StartKeeping started. The strings interned in between stay alive.
func (t *stringTable) EndKeeping() {
	t.keepNew = false
}

// Character returns the string of the ASCII character c.
func (t *stringTable) Character(c byte) Value {
	return t.Ascii[c]
}

// lookup returns the property key of the string s, or nil if s is not interned.
func (t *stringTable) lookup(s wtf8.String) *Object {
	return t.Interned.get(s)
}

// add creates the property key of the string s, which must not be interned, with the shape
// stringShape.
func (t *stringTable) add(s wtf8.String, stringShape *Shape) *Object {
	o := newText(&textObject{
		Kind:       KindString,
		flags:      ObjInterned,
		shape:      stringShape,
		text:       s,
		objectTail: &zeroTail,
	})
	if t.keepNew {
		o.flags |= objKept
	}
	t.Interned.put(s, o, internedMinSweep, isKept)
	return o
}

// internedMinSweep is the number of interned strings below which they are kept alive with the
// agent.
const internedMinSweep = 1 << 10

// internedInitialSize is the number of interned strings that an agent has room for from the start,
// which is more than the built-ins need.
const internedInitialSize = 896

// Intern returns the string s as a property key.
func (r *RealmPart) Intern(s wtf8.String) *Object {
	if o := r.Strings.lookup(s); o != nil {
		return o
	}
	return r.Strings.add(s, r.Shapes.String)
}

// isKept reports whether the interned string o is kept alive with its agent.
func isKept(o *Object) bool {
	return o.flags&objKept != 0
}
