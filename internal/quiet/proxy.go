// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// IsArrayPlain is IsArray of v when v is not a proxy. It reports false as ok for a proxy.
func (q Context) IsArrayPlain(v storage.Value) (is, ok bool) {
	o := v.O
	if o != nil && o.Class == storage.ClassProxy {
		return false, false
	}
	return o != nil && o.Kind == storage.KindArray, true
}
