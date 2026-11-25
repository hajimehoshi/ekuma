// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package quiet

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

func DateNowKernel(q Context, this storage.Value) (storage.Value, bool) {
	// A host clock can reenter JavaScript and observe the native call frame.
	ms, ok := q.SystemUTCEpochMilliseconds()
	return storage.Number(ms), ok
}
