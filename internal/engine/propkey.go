// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

import (
	"github.com/hajimehoshi/ekuma/internal/storage"
)

// propKey returns k as a property key.
func (cx *ExecutionContext) propKey(k storage.QueryKey) storage.PropKey {
	if k.Missing() {
		return storage.PropKey{Key: cx.stringKey(k.Key)}
	}
	return storage.PropKey(k)
}
