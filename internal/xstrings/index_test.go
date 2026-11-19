// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xstrings_test

import (
	"math/bits"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

func TestIndex(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	random := func(n int, alphabet string) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[r.IntN(len(alphabet))]
		}
		return string(b)
	}
	for range 2000 {
		// Periodic strings over small alphabets make many candidates fail late.
		unit := random(1+r.IntN(4), "ab")
		s := strings.Repeat(unit, r.IntN(400)) + random(r.IntN(8), "ab")
		s = s[:r.IntN(len(s)+1)]
		sub := strings.Repeat(unit, r.IntN(40)) + random(r.IntN(4), "ab")
		if r.IntN(4) == 0 && len(s) > 0 {
			i := r.IntN(len(s))
			sub = s[i:min(len(s), i+r.IntN(120))]
		}
		if sub == "" {
			continue
		}
		if got, want := xstrings.Index(s, sub, nil), strings.Index(s, sub); got != want {
			t.Errorf("Index(%q, %q) = %d, want %d", s, sub, got, want)
		}
		if got, want := xstrings.LastIndex(s, sub, nil), strings.LastIndex(s, sub); got != want {
			t.Errorf("LastIndex(%q, %q) = %d, want %d", s, sub, got, want)
		}
	}
}

// TestIndexCollisions searches for a needle that collides with every window of the haystack in
// the rolling hash of strings.Index, which then takes time quadratic in the lengths.
func TestIndexCollisions(t *testing.T) {
	var thueMorse strings.Builder
	for i := range 128 {
		if bits.OnesCount(uint(i))%2 == 0 {
			thueMorse.WriteByte('a')
		} else {
			thueMorse.WriteByte('c')
		}
	}
	sub := strings.Repeat("b", 1<<16-128) + thueMorse.String()
	s := strings.Repeat("b", 1<<23)
	polls := 0
	poll := func() { polls++ }
	if got := xstrings.Index(s, sub, poll); got != -1 {
		t.Errorf("Index = %d, want -1", got)
	}
	if got := xstrings.LastIndex(s, sub, poll); got != -1 {
		t.Errorf("LastIndex = %d, want -1", got)
	}
	if polls == 0 {
		t.Errorf("no polls during the searches of a long string")
	}
}

func TestIndexPolls(t *testing.T) {
	s := strings.Repeat("é", 1<<22)
	for _, sub := range []string{"éz", "z" + strings.Repeat("é", 40)} {
		polls := 0
		poll := func() { polls++ }
		xstrings.Index(s, sub, poll)
		xstrings.LastIndex(s, sub, poll)
		if polls == 0 {
			t.Errorf("no polls while searching for %q", sub[:min(len(sub), 8)])
		}
	}
}
