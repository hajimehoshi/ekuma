// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package numconv

// SetArithLeafWords sets the size of the steps of MulBigInt and QuoRemBigInt, and returns a
// function that restores it.
func SetArithLeafWords(n int) func() {
	old := arithLeafWords
	arithLeafWords = n
	return func() {
		arithLeafWords = old
	}
}
