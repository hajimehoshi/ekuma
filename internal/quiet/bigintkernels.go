// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"math"

	"github.com/hajimehoshi/ekuma/internal/storage"
)

func BigIntConstructorKernel(q Context, this, x storage.Value) (storage.Value, bool) {
	if x.IsNumber() {
		if n := x.Number(); n == math.Trunc(n) && n >= -1<<63 && n < 1<<63 {
			return storage.BigIntFromInt64(int64(n)), true
		}
	}
	return storage.Undefined, false
}
