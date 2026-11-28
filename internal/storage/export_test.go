// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage

import "weak"

// WeakPrimStorage returns weak pointers to the slots and the last block of the secondary
// primitive entries of t.
func WeakPrimStorage(t *WeakTable) (weak.Pointer[int], weak.Pointer[weakPrimEntry]) {
	p := t.prims
	last := p.blocks[len(p.blocks)-1]
	return weak.Make(&p.slots[0]), weak.Make(&last[0])
}
