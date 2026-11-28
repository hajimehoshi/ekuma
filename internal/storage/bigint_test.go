// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package storage_test

import (
	"math/big"
	"math/bits"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/storage"
)

func TestAccumulateBigIntRoom(t *testing.T) {
	small := []big.Word{1, 1}
	acc := storage.NewBigIntFromWords(small, false).AccumulateBigInt(small, false)
	// A sealed accumulator is kept with its room, so a short magnitude must not get the largest room.
	if got, largest := cap(acc.BigInt().Bits()), storage.BigIntInlineWords+1; got >= largest {
		t.Errorf("room of a two-word accumulator: got %d words, want fewer than %d", got, largest)
	}

	large := make([]big.Word, storage.BigIntInlineWords+1)
	large[len(large)-1] = 1
	acc = acc.AccumulateBigInt(large, false)
	want := new(big.Int).Lsh(big.NewInt(1), storage.BigIntInlineWords*bits.UintSize)
	if got := acc.BigInt(); got.Cmp(want) != 0 {
		t.Errorf("accumulator grown past its room: got %v, want %v", got, want)
	}
}
