// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// PropKey is a property key: an array index when key is nil, otherwise an interned string or a symbol.
type PropKey struct {
	key *Object
	idx uint32
}

// IndexKey returns the property key for the array index i.
func IndexKey(i uint32) PropKey {
	return PropKey{idx: i}
}

// NamedKey returns the property key for key, which is an interned string or a symbol.
func NamedKey(key *Object) PropKey {
	return PropKey{key: key}
}

func (k PropKey) isIndex() bool {
	return k.key == nil
}

// IsSymbol reports whether k is a symbol.
func (k PropKey) IsSymbol() bool {
	return k.key != nil && k.key.kind == kindSymbol
}

// KeyOfString returns the property key for the string s.
// It does not keep s.
func (r *realmPart) KeyOfString(s wtf8.String) PropKey {
	if i, ok := stringIndex(s); ok {
		return PropKey{idx: i}
	}
	if o := r.strings.lookup(s); o != nil {
		return PropKey{key: o}
	}
	return PropKey{key: r.Intern(wtf8.String(strings.Clone(string(s))))}
}

// indexName returns the key under which the array index i is stored in a shape.
func (r *realmPart) indexName(i uint32) *Object {
	return r.Intern(wtf8.String(strconv.FormatUint(uint64(i), 10)))
}

// indexKey returns the interned key for the index i, or nil when the key is not interned,
// in which case no object has a property with that key.
func (sp *space) indexKey(i uint32) *Object {
	if i < 10 {
		return sp.strings.character(byte('0' + i)).o
	}
	var buf [10]byte
	return sp.strings.lookup(wtf8.String(strconv.AppendUint(buf[:0], uint64(i), 10)))
}

// QueryKey is a property key for an operation that only reads properties. It may be a string that
// no property key has.
type QueryKey struct {
	// key and idx are as in [PropKey], except that key is not interned when idx is missingKey.
	key *Object
	idx uint32
}

// missingKey is the idx of a query key whose key has the contents of no property key.
const missingKey = 1

func (k QueryKey) missing() bool {
	return k.key != nil && k.idx == missingKey
}

// shortQueryKey returns the query key for the short string v, which is not interned.
func (sp *space) shortQueryKey(v Value) QueryKey {
	s := v.shortText()
	if i, ok := stringIndex(s); ok {
		return QueryKey{idx: i}
	}
	if o := sp.strings.lookup(s); o != nil {
		return QueryKey{key: o}
	}
	return QueryKey{
		key: v.stringObject(),
		idx: missingKey,
	}
}

// queryStringKey returns the query key for the string o.
func (sp *space) queryStringKey(o *Object) QueryKey {
	c := o.Str()
	if len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
		if i, ok := o.arrayIndex(); ok {
			return QueryKey{idx: i}
		}
	}
	if o.flags&objInterned != 0 {
		return QueryKey{key: o}
	}
	if k, ok := o.extObject(); ok {
		return QueryKey{key: k}
	}
	if k := sp.strings.lookup(c); k != nil {
		o.setExtState(k)
		return QueryKey{key: k}
	}
	// Not interning o keeps a read of a missing key from adding to the interned strings.
	return QueryKey{
		key: o,
		idx: missingKey,
	}
}

// propKey returns k as a property key.
func (cx *ExecutionContext) propKey(k QueryKey) PropKey {
	if k.missing() {
		return PropKey{key: cx.stringKey(k.key)}
	}
	return PropKey(k)
}

// isEnumerableString reports whether key is a string and flags make the property enumerable.
func isEnumerableString(key *Object, flags uint8) bool {
	return key.kind == kindString && flags&FlagEnumerable != 0
}

// enumerableKey returns key as a property key when it is a string and flags make it enumerable,
// and the zero PropKey otherwise.
func enumerableKey(key *Object, flags uint8) PropKey {
	if isEnumerableString(key, flags) {
		return PropKey{key: key}
	}
	return PropKey{}
}
