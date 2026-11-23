// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection

// EntryStorage returns the number of entries that the storage of t has room for.
func EntryStorage(t *Table) int {
	return t.capacity()
}

// BlockPointerStorage returns the number of pointers to blocks of entries that t has room for,
// and the first place of that storage, which identifies it, or nil when there is none.
func BlockPointerStorage(t *Table) (int, **[blockSize]entry) {
	if cap(t.blocks) == 0 {
		return 0, nil
	}
	return cap(t.blocks), &t.blocks[:1][0]
}
