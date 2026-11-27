// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import (
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/internal/numconv"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// PropKey is a property key: an array index when Key is nil, otherwise an interned string or a symbol.
type PropKey struct {
	Key *Object
	Idx uint32
}

// IndexKey returns the property key for the array index i.
func IndexKey(i uint32) PropKey {
	return PropKey{Idx: i}
}

// NamedKey returns the property key for key, which is an interned string or a symbol.
func NamedKey(key *Object) PropKey {
	return PropKey{Key: key}
}

func (k PropKey) IsIndex() bool {
	return k.Key == nil
}

// IsSymbol reports whether k is a symbol.
func (k PropKey) IsSymbol() bool {
	return k.Key != nil && k.Key.Kind == KindSymbol
}

// KeyOfString returns the property key for the string s.
// It does not keep s.
func (r *RealmPart) KeyOfString(s wtf8.String) PropKey {
	if i, ok := StringIndex(s); ok {
		return PropKey{Idx: i}
	}
	if o := r.Strings.lookup(s); o != nil {
		return PropKey{Key: o}
	}
	return PropKey{Key: r.Intern(wtf8.String(strings.Clone(string(s))))}
}

// IndexName returns the key under which the array index i is stored in a shape.
func (r *RealmPart) IndexName(i uint32) *Object {
	return r.Intern(wtf8.String(strconv.FormatUint(uint64(i), 10)))
}

// IndexKey returns the interned key for the index i, or nil when the key is not interned,
// in which case no object has a property with that key.
func (sp *Space) IndexKey(i uint32) *Object {
	if i < 10 {
		return sp.Strings.Character(byte('0' + i)).O
	}
	var buf [10]byte
	return sp.Strings.lookup(wtf8.String(strconv.AppendUint(buf[:0], uint64(i), 10)))
}

// QueryKey is a property key for an operation that only reads properties. It may be a string that
// no property key has.
type QueryKey struct {
	// Key and Idx are as in [PropKey], except that Key is not interned when Idx is MissingKey.
	Key *Object
	Idx uint32
}

// MissingKey is the Idx of a query key whose Key has the contents of no property key.
const MissingKey = 1

func (k QueryKey) Missing() bool {
	return k.Key != nil && k.Idx == MissingKey
}

// ShortQueryKey returns the query key for the short string v, which is not interned.
func (sp *Space) ShortQueryKey(v Value) QueryKey {
	s := v.ShortText()
	if i, ok := StringIndex(s); ok {
		return QueryKey{Idx: i}
	}
	if o := sp.Strings.lookup(s); o != nil {
		return QueryKey{Key: o}
	}
	return QueryKey{
		Key: v.StringObject(),
		Idx: MissingKey,
	}
}

// QueryStringKey returns the query key for the string o.
func (sp *Space) QueryStringKey(o *Object) QueryKey {
	c := o.Str()
	if len(c) > 0 && c[0] >= '0' && c[0] <= '9' {
		if i, ok := o.ArrayIndex(); ok {
			return QueryKey{Idx: i}
		}
	}
	if o.flags&ObjInterned != 0 {
		return QueryKey{Key: o}
	}
	if k, ok := o.ExtObject(); ok {
		return QueryKey{Key: k}
	}
	if k := sp.Strings.lookup(c); k != nil {
		o.SetExtState(k)
		return QueryKey{Key: k}
	}
	// Not interning o keeps a read of a missing key from adding to the interned strings.
	return QueryKey{
		Key: o,
		Idx: MissingKey,
	}
}

// isEnumerableString reports whether key is a string and flags make the property enumerable.
func isEnumerableString(key *Object, flags uint8) bool {
	return key.Kind == KindString && flags&FlagEnumerable != 0
}

// EnumerableKey returns key as a property key when it is a string and flags make it enumerable,
// and the zero PropKey otherwise.
func EnumerableKey(key *Object, flags uint8) PropKey {
	if isEnumerableString(key, flags) {
		return PropKey{Key: key}
	}
	return PropKey{}
}

// maxNumberStringLen bounds the length of the string of a number, such as
// "-1.2345678901234567e-308".
const maxNumberStringLen = 32

// IsCanonicalNumericString reports whether CanonicalNumericIndexString returns a number for the string key.
// The array indices are not named keys, so they are not considered.
func IsCanonicalNumericString(key *Object) bool {
	if key.Kind != KindString {
		return false
	}
	s := key.Text()
	if len(s) == 0 {
		return false
	}
	if c := s[0]; c != '-' && c != 'I' && c != 'N' && (c < '0' || c > '9') {
		return false
	}
	if s == "-0" {
		return true
	}
	// The string of a number is short.
	if len(s) > maxNumberStringLen {
		return false
	}
	return numconv.NumberToString(numconv.StringToNumber(s, nil)) == s
}

// AppendIndexRun appends the keys of the indices from i up to j to keys.
func AppendIndexRun(keys []PropKey, i, j int) []PropKey {
	for ; i < j; i++ {
		keys = append(keys, IndexKey(uint32(i)))
	}
	return keys
}

// AppendElemRun appends the keys of the elements of o from start to end to keys.
func AppendElemRun(keys []PropKey, o *Object, start, end int) []PropKey {
	if o.packedNums() != nil {
		return AppendIndexRun(keys, start, end)
	}
	for i, e := range o.elems[start:end] {
		if !e.IsHole() {
			keys = append(keys, IndexKey(uint32(start+i)))
		}
	}
	return keys
}

// AppendEnumerableNames appends the enumerable string keys among names, which are keys of the
// private shape s, to keys.
func AppendEnumerableNames(keys []PropKey, s *Shape, names []*Object) []PropKey {
	for _, k := range names {
		if k != nil && k.Kind == KindString && s.Table[k].Flags&FlagEnumerable != 0 {
			keys = append(keys, NamedKey(k))
		}
	}
	return keys
}
