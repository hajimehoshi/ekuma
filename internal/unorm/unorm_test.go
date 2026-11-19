// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package unorm_test

import (
	"math/rand/v2"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"github.com/hajimehoshi/ekuma/internal/unorm"
)

var forms = []norm.Form{norm.NFC, norm.NFD, norm.NFKC, norm.NFKD}

func TestStringInSteps(t *testing.T) {
	// Short runs of non-starters normalize as the package norm does, also across the steps of a
	// long string.
	r := rand.New(rand.NewPCG(1, 2))
	pieces := []string{"a", "é", "é", "̧́", "가", "가", "ﬁ", "ḍ̇", "x"}
	var b strings.Builder
	for b.Len() < 1<<18 {
		b.WriteString(pieces[r.IntN(len(pieces))])
	}
	s := b.String()
	for _, form := range forms {
		if got, want := unorm.String(form, s, nil), form.String(s); got != want {
			t.Errorf("form %v: the result differs from the package norm", form)
		}
	}
}

func TestStringLongRun(t *testing.T) {
	// A run of non-starters without any boundary is sorted by combining class.
	const n = 1 << 16
	s := "a" + strings.Repeat("̧̨́", n)
	for _, tc := range []struct {
		form norm.Form
		want string
	}{
		{
			form: norm.NFD,
			want: "a" + strings.Repeat("̧̨", n) + strings.Repeat("́", n),
		},
		{
			form: norm.NFC,
			want: "á" + strings.Repeat("̧̨", n) + strings.Repeat("́", n-1),
		},
	} {
		var polls int
		got := unorm.String(tc.form, s+"b", func() { polls++ })
		if got != tc.want+"b" {
			t.Errorf("form %v: got %d bytes, want %d", tc.form, len(got), len(tc.want)+1)
		}
		if polls < 8 {
			t.Errorf("form %v: got %d polls, want at least 8", tc.form, polls)
		}
	}
}
