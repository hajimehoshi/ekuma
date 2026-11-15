// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package cldr_test

import (
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
)

func TestRecords(t *testing.T) {
	for _, r := range recordSums {
		h := sha256.New()
		for i := range r.m.Len() {
			k := r.m.Key(i)
			v, ok := r.m.Lookup(k)
			if !ok {
				t.Errorf("%s: Lookup(%q) reports no key", r.name, k)
			}
			if v != r.m.Value(i) {
				t.Errorf("%s: Lookup(%q) and Value(%d) differ", r.name, k, i)
			}
			fmt.Fprintf(h, "%s\x00%s\x00", k, v)
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != r.sum {
			t.Errorf("%s: checksum %s, want %s", r.name, got, r.sum)
		}
	}
}

func TestRecordsConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for _, r := range recordSums {
				for _, k := range []string{"ja-JP", "ja", "de", "zh-Hant-TW", "sr-Latn"} {
					v, ok := r.m.Lookup(k)
					want := ""
					for i := range r.m.Len() {
						if r.m.Key(i) == k {
							want = r.m.Value(i)
						}
					}
					if ok && v != want {
						t.Errorf("%s: Lookup(%q) is not Value of its key", r.name, k)
					}
				}
			}
		})
	}
	wg.Wait()
}
