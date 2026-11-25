// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

// denseFactor is the largest number of slots per element for which ExecutionContext.densify moves the array
// index properties of an object into elems.
const denseFactor = 4

// AppendIndexRun appends the keys of the indices from i up to j to keys.
func AppendIndexRun(keys []PropKey, i, j int) []PropKey {
	for ; i < j; i++ {
		keys = append(keys, IndexKey(uint32(i)))
	}
	return keys
}

// AppendElemRun appends the keys of the elements in elems, the first of which has the index
// base, to keys.
func AppendElemRun(keys []PropKey, elems []Value, base int) []PropKey {
	for i, e := range elems {
		if !e.IsHole() {
			keys = append(keys, IndexKey(uint32(base+i)))
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
