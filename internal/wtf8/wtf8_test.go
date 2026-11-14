// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package wtf8_test

import (
	"cmp"
	"slices"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// samples are code unit sequences that cover ASCII, two- and three-byte code points,
// pairs, and lone surrogates in every position.
var samples = [][]uint16{
	{},
	{'a'},
	{'a', 0xE9, 0x3042, 'z'},
	{0xD83D, 0xDE00},
	{'a', 0xD83D, 0xDE00, 'b', 0xD83D, 0xDE01},
	{0xD83D},
	{0xDE00},
	{0xDE00, 0xD83D},
	{0xD83D, 0xD83D, 0xDE00, 0xDE00},
	{0xE000, 0xFFFF, 0xD800, 0xDC00},
}

func TestRoundTrip(t *testing.T) {
	for _, u := range samples {
		s := wtf8.FromUTF16(u)
		if got := wtf8.AppendUTF16(nil, s); !slices.Equal(got, u) {
			t.Errorf("AppendUTF16(FromUTF16(%x)) = %x", u, got)
		}
		if got, want := wtf8.Len(s), len(u); got != want {
			t.Errorf("Len(FromUTF16(%x)) = %d, want %d", u, got, want)
		}
	}
}

func TestSeekAtSlice(t *testing.T) {
	for _, u := range samples {
		s := wtf8.FromUTF16(u)
		// Seek from every position to every position, so both directions and both halves are covered.
		for from := 0; from <= len(u); from++ {
			start := wtf8.Seek(s, wtf8.Pos{}, from)
			for i := 0; i <= len(u); i++ {
				p := wtf8.Seek(s, start, i)
				if p.Unit != i {
					t.Errorf("Seek(%x, from %d, %d).Unit = %d", u, from, i, p.Unit)
					continue
				}
				if i < len(u) {
					if got := wtf8.At(s, p); got != u[i] {
						t.Errorf("At(%x, %d) = %x, want %x (from %d)", u, i, got, u[i], from)
					}
				}
				if from <= i {
					got := wtf8.Slice(s, start, p)
					if want := wtf8.FromUTF16(u[from:i]); got != want {
						t.Errorf("Slice(%x, %d, %d) = %x, want %x", u, from, i, got, want)
					}
				}
			}
		}
	}
}

func TestConcat(t *testing.T) {
	for _, a := range samples {
		for _, b := range samples {
			got := wtf8.Concat(wtf8.FromUTF16(a), wtf8.FromUTF16(b))
			want := wtf8.FromUTF16(append(slices.Clone(a), b...))
			if got != want {
				t.Errorf("Concat(%x, %x) = %x, want %x", a, b, got, want)
			}
		}
	}
}

func TestWriteRepeated(t *testing.T) {
	for _, a := range samples {
		for _, s := range samples {
			for _, count := range []int{1, 5} {
				var b wtf8.Builder
				b.WriteString(wtf8.FromUTF16(a))
				b.WriteRepeated(wtf8.FromUTF16(s), count)
				want := slices.Clone(a)
				for range count {
					want = append(want, s...)
				}
				if got := b.String(); got != wtf8.FromUTF16(want) {
					t.Errorf("WriteRepeated(%x, %d) after %x = %x, want %x", s, count, a, got, wtf8.FromUTF16(want))
				}
			}
		}
	}
}

func TestCompare(t *testing.T) {
	for _, a := range samples {
		for _, b := range samples {
			got := wtf8.Compare(wtf8.FromUTF16(a), wtf8.FromUTF16(b))
			want := slices.CompareFunc(a, b, cmp.Compare[uint16])
			if got != want {
				t.Errorf("Compare(%x, %x) = %d, want %d", a, b, got, want)
			}
		}
	}
}

func TestToValidUTF8(t *testing.T) {
	got := wtf8.ToValidUTF8(wtf8.FromUTF16([]uint16{'a', 0xD83D, 'b', 0xD83D, 0xDE00, 0xDE00}))
	want := "a�b\U0001F600�"
	if got != want {
		t.Errorf("ToValidUTF8 = %q, want %q", got, want)
	}
}
