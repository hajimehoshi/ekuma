// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package collection

// EntryStorage returns the number of entries that the storage of t has room for.
func EntryStorage(t *Table) int {
	return t.capacity()
}
