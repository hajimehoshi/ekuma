// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package cldr

// BrokenMap returns a map with a truncated compressed block.
func BrokenMap() Map {
	return Map{
		keys: Strings{
			data: "key",
			ends: []uint32{3},
		},
		values: Strings{
			ends: []uint32{0},
		},
		blocks: &blocks{
			firstKeys: []uint32{0},
			dataEnds:  []uint32{0},
			valueEnds: []uint32{1},
			inflated:  make([]inflatedBlock, 1),
		},
	}
}
