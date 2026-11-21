// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package segment

import (
	"compress/flate"
	"encoding/binary"
	"io"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/hajimehoshi/ekuma/internal/unicode/wordbreak"
)

type trieNode struct {
	firstChild uint32
	label      uint16
	cost       byte
}

type wordTrie []trieNode

func loadTrie(data string) wordTrie {
	r := flate.NewReader(strings.NewReader(data))
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	// A sentinel delimits the last child range.
	n := len(b) / 5
	nodes := make(wordTrie, n+1)
	first := uint32(1)
	for i := range n {
		nodes[i] = trieNode{
			firstChild: first,
			label:      binary.LittleEndian.Uint16(b[2*i:]),
			cost:       b[2*n+i],
		}
		first += uint32(binary.LittleEndian.Uint16(b[3*n+2*i:]))
	}
	nodes[n].firstChild = first
	return nodes
}

var thaiTrie = sync.OnceValue(func() wordTrie { return loadTrie(wordbreak.Thai) })
var hanTrie = sync.OnceValue(func() wordTrie { return loadTrie(wordbreak.Han) })

func (t wordTrie) child(parent int, r rune) int {
	if r > 0xffff {
		return -1
	}
	start, end := t[parent].firstChild, t[parent+1].firstChild
	children := t[start:end]
	i := sort.Search(len(children), func(i int) bool { return children[i].label >= uint16(r) })
	if i == len(children) || children[i].label != uint16(r) {
		return -1
	}
	return int(start) + i
}

type wordScript int

const (
	otherScript wordScript = iota
	thaiScript
	hanScript
)

func scriptOf(r rune) wordScript {
	switch {
	case r < 0x0e01:
		return otherScript
	case r <= 0x0e4e && unicode.IsLetter(r):
		return thaiScript
	case unicode.Is(unicode.Han, r) && !unicode.IsMark(r):
		return hanScript
	}
	return otherScript
}

type dictionaryPoller struct {
	poll  func()
	steps int
}

func (p *dictionaryPoller) step() {
	p.steps++
	if p.steps%pollSteps == 0 && p.poll != nil {
		p.poll()
	}
}

func dictionaryBoundaries(rs []rune, ps []uint16, bounds []int32, poll func()) []int32 {
	p := dictionaryPoller{
		poll: poll,
	}
	out := bounds[:0]
	for i := 0; i < len(bounds)-1; {
		p.step()
		script := scriptOf(rs[bounds[i]])
		j := i + 1
		if script != otherScript {
			for j < len(bounds)-1 && scriptOf(rs[bounds[j]]) == script {
				p.step()
				j++
			}
		}
		if j == i+1 {
			out = append(out, bounds[i])
		} else {
			out = appendDictionaryWords(out, rs, ps, bounds[i:j+1], script, &p)
		}
		i = j
	}
	return append(out, bounds[len(bounds)-1])
}

// wordSteps chooses the cheapest segmentation of each suffix. A positive step spans
// that many UAX #29 segments. A zero step takes one segment as an unknown word.
func wordSteps(rs []rune, ps []uint16, bounds []int32, script wordScript, p *dictionaryPoller) []byte {
	n := len(bounds) - 1
	var trie wordTrie
	// ICU's Han entries carry word costs. For Thai, minimize the number of
	// unmatched segments first, then the number of dictionary words.
	unknownCost := int64(255)
	if script == thaiScript {
		trie = thaiTrie()
		unknownCost = int64(n) + 1
	} else {
		trie = hanTrie()
	}

	// costs holds the cheapest suffix starting at each candidate boundary.
	// A dictionary word spans at most 255 runes, with at least one matched
	// rune per segment, so only the next 255 suffix costs are needed.
	var costs [256]int64
	steps := make([]byte, n)
	for i := n - 1; i >= 0; i-- {
		best := unknownCost + costs[byte(i+1)]
		var node int
	match:
		for j := i; j < n; j++ {
			for k := bounds[j]; k < bounds[j+1]; k++ {
				p.step()
				// Thai combining marks are part of dictionary spellings.
				if wordIgnorable(wordOf(ps[k])) && !unicode.Is(unicode.Thai, rs[k]) {
					continue
				}
				node = trie.child(node, rs[k])
				if node < 0 {
					break match
				}
			}
			if cost := trie[node].cost; cost != 0 {
				total := int64(cost) + costs[byte(j+1)]
				if total <= best {
					best = total
					steps[i] = byte(j + 1 - i)
				}
			}
		}
		costs[byte(i)] = best
	}

	return steps
}

func appendDictionaryWords(out []int32, rs []rune, ps []uint16, bounds []int32, script wordScript, p *dictionaryPoller) []int32 {
	steps := wordSteps(rs, ps, bounds, script, p)
	var previousUnknown bool
	for i := 0; i < len(steps); i += max(1, int(steps[i])) {
		p.step()
		unknown := script == thaiScript && steps[i] == 0
		if unknown && i > 0 && thaiSuffix(rs[bounds[i]-1], rs[bounds[i]]) {
			continue
		}
		if !unknown || !previousUnknown {
			out = append(out, bounds[i])
		}
		previousUnknown = unknown
	}
	return out
}

// Thai abbreviation and repetition signs can follow a word in that order.
func thaiSuffix(prev, r rune) bool {
	return r == 'ฯ' && prev != 'ฯ' && prev != 'ๆ' || r == 'ๆ' && prev != 'ๆ'
}
