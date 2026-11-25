// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func ObjectHasOwnPropertyKernel(q Context, this, k storage.Value) (storage.Value, bool) {
	if !this.IsObject() {
		return storage.Undefined, false
	}
	has, ok := q.HasOwnPropertyPlain(this.Object(), k)
	return storage.Bool(has), ok
}
