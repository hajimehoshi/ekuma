// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package engine

// propKey returns k as a property key.
func (cx *ExecutionContext) propKey(k QueryKey) PropKey {
	if k.Missing() {
		return PropKey{Key: cx.stringKey(k.Key)}
	}
	return PropKey(k)
}
