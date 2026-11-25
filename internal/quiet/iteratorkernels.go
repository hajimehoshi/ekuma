// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

func StringIteratorStep(q Context, _ *storage.Object, it *StringIterator) (storage.Value, bool) {
	if it.pos >= len(it.Str) {
		return storage.Undefined, false
	}
	_, size := wtf8.DecodeRune(it.Str[it.pos:])
	s := it.Str[it.pos : it.pos+size]
	it.pos += size
	return q.NewString(s), true
}
