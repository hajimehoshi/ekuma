// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package wtf8_test

import (
	"cmp"
	"slices"
	"testing"
	"unicode/utf16"

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

// TestWordScans checks the functions that scan a word at a time with code points and lone
// surrogates at every offset from the start of a word.
func TestWordScans(t *testing.T) {
	for pad := range 17 {
		for _, u := range samples {
			units := make([]uint16, pad, pad+2*len(u))
			for i := range units {
				units[i] = 'a'
			}
			units = append(units, u...)
			units = append(units, u...)
			s := wtf8.FromUTF16(units)
			if got, want := wtf8.Len(s), len(units); got != want {
				t.Errorf("Len(FromUTF16(%x)) = %d, want %d", units, got, want)
			}
			// Decode makes one rune of each pair and of each lone surrogate.
			if got, want := wtf8.CountCodePoints(s), len(utf16.Decode(units)); got != want {
				t.Errorf("CountCodePoints(FromUTF16(%x)) = %d, want %d", units, got, want)
			}
			ascii := !slices.ContainsFunc(units, func(c uint16) bool { return c >= 0x80 })
			if got := wtf8.IsASCII(s); got != ascii {
				t.Errorf("IsASCII(FromUTF16(%x)) = %t, want %t", units, got, ascii)
			}
		}
	}
}

func TestToLowerASCII(t *testing.T) {
	const s = "ÉA\xed\xa0\x80Z\xff"
	const want = "Éa\xed\xa0\x80z\xff"
	if got := wtf8.ToLowerASCII(s); got != want {
		t.Errorf("ToLowerASCII(%q) = %q, want %q", s, got, want)
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

func TestStringBuilderSnapshot(t *testing.T) {
	var b wtf8.StringBuilder
	b.WriteString("x")
	b.WriteRune(0xD83D)
	snapshot := b.String()
	b.WriteRune(0xDE00)
	if got, want := b.String(), wtf8.String("x😀"); got != want {
		t.Errorf("String() after the low surrogate = %q, want %q", got, want)
	}
	if want := wtf8.String("x\xed\xa0\xbd"); snapshot != want {
		t.Errorf("snapshot = %q, want %q", snapshot, want)
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
	// A common prefix longer than the chunks that Compare skips, which ends inside one.
	var long []uint16
	for i := range 35 {
		long = append(long, 'a'+uint16(i%3), 0x3042)
	}
	for _, prefix := range [][]uint16{nil, long} {
		for _, a := range samples {
			for _, b := range samples {
				a, b := slices.Concat(prefix, a), slices.Concat(prefix, b)
				got := wtf8.Compare(wtf8.FromUTF16(a), wtf8.FromUTF16(b))
				want := slices.CompareFunc(a, b, cmp.Compare[uint16])
				if got != want {
					t.Errorf("Compare(%x, %x) = %d, want %d", a, b, got, want)
				}
			}
		}
	}
}

func TestToValidUTF8(t *testing.T) {
	// The last two lone surrogates are in a row, and each becomes a U+FFFD of its own.
	got := wtf8.ToValidUTF8(wtf8.FromUTF16([]uint16{'a', 0xD83D, 'b', 0xD83D, 0xDE00, 0xDE00, 0xD83D}))
	want := "a�b\U0001F600��"
	if got != want {
		t.Errorf("ToValidUTF8 = %q, want %q", got, want)
	}
}

func TestWriteStringBytes(t *testing.T) {
	for _, tc := range []struct {
		prefix wtf8.String
		want   wtf8.String
	}{
		{
			prefix: "\xed\xa0\xbd",
			want:   "😀x",
		},
		{
			prefix: "a",
			want:   "a\xed\xb8\x80x",
		},
	} {
		var b wtf8.Builder
		b.WriteString(tc.prefix)
		b.WriteStringBytes([]byte("\xed\xb8\x80x"))
		if got := b.String(); got != tc.want {
			t.Errorf("WriteStringBytes after %q = %q, want %q", tc.prefix, got, tc.want)
		}
	}
}
