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
var cjTrie = sync.OnceValue(func() wordTrie { return loadTrie(wordbreak.CJ) })
var khmerTrie = sync.OnceValue(func() wordTrie { return loadTrie(wordbreak.Khmer) })
var burmeseTrie = sync.OnceValue(func() wordTrie { return loadTrie(wordbreak.Burmese) })

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
	cjScript
	khmerScript
	burmeseScript
)

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

// appendDictionaryBreaks adds the boundaries strictly inside each dictionary
// run of a single rule-based word.
func appendDictionaryBreaks(out []int32, rs []rune, ps []propertyIndex, start, end int, p *dictionaryPoller) []int32 {
	for i := start; i < end; {
		p.step()
		script := scriptOf(ps[i])
		j := i + 1
		for j < end && scriptOf(ps[j]) == script {
			p.step()
			j++
		}
		if j-i > 1 {
			switch script {
			case cjScript:
				out = appendCJBreaks(out, rs[i:j], i, p)
			case thaiScript, khmerScript, burmeseScript:
				out = appendSoutheastAsianBreaks(out, rs[i:j], ps[i:j], i, script, p)
			}
		}
		i = j
	}
	return out
}
