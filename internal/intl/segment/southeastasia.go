// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package segment

import "slices"

// candidates returns matching word lengths in ascending order and the number
// of runes examined before the trie stopped matching.
func (t wordTrie) candidates(rs []rune, lengths []int) ([]int, int) {
	lengths = lengths[:0]
	var node int
	for i, r := range rs {
		node = t.child(node, r)
		if node < 0 {
			return lengths, i + 1
		}
		if t[node].cost != 0 {
			lengths = append(lengths, i+1)
			if t[node].firstChild == t[node+1].firstChild {
				return lengths, i + 1
			}
		}
	}
	return lengths, len(rs)
}

// chooseWord prefers a dictionary word that reaches the end of the run or
// starts a three-word chain. Candidates at each position are tried longest first.
func (t wordTrie) chooseWord(rs []rune) int {
	var first, second, third [20]int
	words, _ := t.candidates(rs, first[:0])
	if len(words) == 0 {
		return 0
	}
	best := words[len(words)-1]
	if len(words) == 1 || best == len(rs) {
		return best
	}
	for _, n := range slices.Backward(words) {
		following, _ := t.candidates(rs[n:], second[:0])
		if len(following) == 0 {
			continue
		}
		best = n
		for _, length := range slices.Backward(following) {
			end := n + length
			if end == len(rs) {
				return n
			}
			if next, _ := t.candidates(rs[end:], third[:0]); len(next) > 0 {
				return n
			}
		}
	}
	return best
}

func canResumeWord(script wordScript, prev, next rune) bool {
	switch script {
	case thaiScript:
		return prev != 0x0e31 && !(0x0e40 <= prev && prev <= 0x0e44) &&
			(0x0e01 <= next && next <= 0x0e2e || 0x0e40 <= next && next <= 0x0e44)
	case khmerScript:
		return prev != 0x17d2 && 0x1780 <= next && next <= 0x17b3
	default:
		return 0x1000 <= next && next <= 0x102a
	}
}

func appendSoutheastAsianBreaks(out []Boundary, rs []rune, ps []propertyIndex, start int, script wordScript, p *dictionaryPoller) []Boundary {
	if len(rs) < 4 || script == thaiScript && len(rs) == 4 {
		return out
	}
	var t wordTrie
	switch script {
	case thaiScript:
		t = thaiTrie()
	case khmerScript:
		t = khmerTrie()
	case burmeseScript:
		t = burmeseTrie()
	}
	var scratch [20]int
	for i := 0; i < len(rs); {
		p.step()
		n := t.chooseWord(rs[i:])
		end := i + n
		// A short word may absorb an unknown suffix. Resume where the script
		// permits a new word and the dictionary recognizes one.
		if n < 3 && end < len(rs) {
			next, prefix := t.candidates(rs[end:], scratch[:0])
			if len(next) == 0 && (n == 0 || prefix < 3) {
				end++
				for end < len(rs) {
					p.step()
					if canResumeWord(script, rs[end-1], rs[end]) {
						if next, _ := t.candidates(rs[end:], scratch[:0]); len(next) > 0 {
							break
						}
					}
					end++
				}
			}
		}
		for end < len(rs) && dictionaryMark(ps[end]) {
			p.step()
			end++
		}
		if script == thaiScript && end < len(rs) && (rs[end] == 'ฯ' || rs[end] == 'ๆ') {
			if next, _ := t.candidates(rs[end:], scratch[:0]); len(next) == 0 {
				if rs[end] == 'ฯ' && rs[end-1] != 'ฯ' && rs[end-1] != 'ๆ' {
					end++
				}
				if end < len(rs) && rs[end] == 'ๆ' && rs[end-1] != 'ๆ' {
					end++
				}
			}
		}
		if end < len(rs) {
			out = append(out, boundaryAt(start+end))
		}
		i = end
	}
	return out
}
