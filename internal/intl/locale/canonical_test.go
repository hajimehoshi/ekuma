// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

// TestCanonicalizeManySubtags checks a tag with many attributes and keywords, which the poll
// function can stop.
func TestCanonicalizeManySubtags(t *testing.T) {
	var b strings.Builder
	b.WriteString("en-u")
	// The attributes come in a scrambled order, each twice.
	const n = 1 << 16
	for i := range 2 * n {
		fmt.Fprintf(&b, "-a%05d", i*7919%n)
	}
	for i := range n {
		fmt.Fprintf(&b, "-k%c-abc", 'a'+i%26)
	}
	tag := b.String()

	var want strings.Builder
	want.WriteString("en-u")
	for i := range n {
		fmt.Fprintf(&want, "-a%05d", i)
	}
	for c := 'a'; c <= 'z'; c++ {
		fmt.Fprintf(&want, "-k%c-abc", c)
	}
	polls := 0
	if got, ok := locale.Canonicalize(tag, func() { polls++ }); !ok || got != want.String() {
		t.Errorf("Canonicalize: got %.40q..., %t", got, ok)
	}
	if polls == 0 {
		t.Error("Canonicalize did not poll")
	}

	type stop struct{}
	defer func() {
		if _, ok := recover().(stop); !ok {
			t.Error("Canonicalize did not stop")
		}
	}()
	locale.Canonicalize(tag, func() { panic(stop{}) })
}
