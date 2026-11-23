// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package segment

import (
	"slices"

	"github.com/hajimehoshi/ekuma/internal/unorm"
)

// normalizeCJ maps normalized rune indices back to the start of each source
// normalization segment. A nil map denotes the identity.
func normalizeCJ(rs []rune, p *dictionaryPoller) ([]rune, []int32) {
	if unorm.QuickCheck(unorm.NFKC, string(rs), p.poll) {
		return rs, nil
	}
	var normalized []rune
	var source []int32
	for i := 0; i < len(rs); {
		j := i + 1
		for j < len(rs) && !unorm.BoundaryBefore(unorm.NFKC, rs[j]) {
			p.step()
			j++
		}
		for _, r := range unorm.String(unorm.NFKC, string(rs[i:j]), p.poll) {
			p.step()
			normalized = append(normalized, r)
			source = append(source, int32(i))
		}
		i = j
	}
	return normalized, source
}

func isKatakana(r rune) bool {
	return 0x30a1 <= r && r <= 0x30fe && r != 0x30fb || 0xff66 <= r && r <= 0xff9f
}

func appendCJBreaks(out []Boundary, input []rune, start int, p *dictionaryPoller) []Boundary {
	rs, source := normalizeCJ(input, p)
	trie := cjTrie()
	// At each end position keep the lowest-cost path and the length of its
	// last word. Dictionary matches and Katakana runs span at most 20 runes.
	const maxWord = 20
	var costs [maxWord + 1]int64
	lengths := make([]byte, len(rs)+1)
	for i, r := range rs {
		p.step()
		add := func(length, cost int) {
			end := i + length
			total := costs[i%len(costs)] + int64(cost)
			if lengths[end] == 0 || total < costs[end%len(costs)] {
				lengths[end] = byte(length)
				costs[end%len(costs)] = total
			}
		}
		node := trie.child(0, r)
		if node < 0 || trie[node].cost == 0 {
			add(1, 255)
		}
		for n := 1; node >= 0; n++ {
			if cost := trie[node].cost; cost != 0 {
				add(n, int(cost))
			}
			if n == maxWord || i+n == len(rs) {
				break
			}
			node = trie.child(node, rs[i+n])
		}

		if isKatakana(r) && (i == 0 || !isKatakana(rs[i-1])) {
			n := 1
			for n < maxWord && i+n < len(rs) && isKatakana(rs[i+n]) {
				n++
			}
			if n < maxWord {
				// ICU's costs for a Katakana run, indexed by rune count.
				short := [...]int{0, 984, 408, 240, 204, 252, 300, 372, 480}
				cost := 8192
				if n < len(short) {
					cost = short[n]
				}
				add(n, cost)
			}
		}
	}

	base := len(out)
	for end := len(rs); end > 0; {
		p.step()
		end -= int(lengths[end])
		pos := int32(end)
		if source != nil {
			pos = source[end]
		}
		b := boundaryAt(start + int(pos))
		if pos > 0 && (len(out) == base || out[len(out)-1] != b) {
			out = append(out, b)
		}
	}
	slices.Reverse(out[base:])
	return out
}
