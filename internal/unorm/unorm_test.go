// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package unorm_test

import (
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/unorm"
)

func TestStringInSteps(t *testing.T) {
	// Each repetition starts with a starter, so its expected form is independent
	// of its neighbors even when a chunk boundary falls in the middle of it.
	const count = 1 << 14
	s := strings.Repeat("e\u0301가ﬁḋ\u0323", count)
	for _, tc := range []struct {
		form unorm.Form
		want string
	}{
		{
			form: unorm.NFC,
			want: "é가ﬁḍ\u0307",
		},
		{
			form: unorm.NFD,
			want: "e\u0301가ﬁd\u0323\u0307",
		},
		{
			form: unorm.NFKC,
			want: "é가fiḍ\u0307",
		},
		{
			form: unorm.NFKD,
			want: "e\u0301가fid\u0323\u0307",
		},
	} {
		if got := unorm.String(tc.form, s, nil); got != strings.Repeat(tc.want, count) {
			t.Errorf("form %v: incorrect normalization across chunks", tc.form)
		}
	}
}

func TestStringLongRun(t *testing.T) {
	// A run of non-starters without any boundary is sorted by combining class.
	const n = 1 << 16
	s := "a" + strings.Repeat("̧̨́", n)
	for _, tc := range []struct {
		form unorm.Form
		want string
	}{
		{
			form: unorm.NFD,
			want: "a" + strings.Repeat("̧̨", n) + strings.Repeat("́", n),
		},
		{
			form: unorm.NFC,
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

func TestCombiningMarkAtChunkBoundary(t *testing.T) {
	// U+1ACF has combining class 230. The following dot
	// below moves across the chunk boundary and composes with the A.
	const end = "A\u0315\u1acf"
	prefix := strings.Repeat("x", (1<<16)-len(end))
	got := unorm.String(unorm.NFC, prefix+end+"\u0323", nil)
	if want := prefix + "Ạ\u1acf\u0315"; got != want {
		t.Errorf("normalized suffix = %+q, want %+q", strings.TrimPrefix(got, prefix), strings.TrimPrefix(want, prefix))
	}
}
