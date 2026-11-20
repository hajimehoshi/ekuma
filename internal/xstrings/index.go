// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package xstrings

import (
	"strings"

	"github.com/hajimehoshi/ekuma/internal/wtf8"
)

// shortSub is the length up to which strings.Index and strings.LastIndex spend at most a
// constant amount of work per byte of the string searched.
const shortSub = 32

// searchChunk is the number of bytes of a string that a search looks at between two calls of its
// poll function.
const searchChunk = 1 << 20

// Index returns the index of the first instance of sub in s, or -1, in time linear in the lengths
// of s and sub. poll, which may be nil, is called now and then during a long search, and may stop
// the search by panicking.
func Index(s, sub wtf8.String, poll func()) int {
	n := len(sub)
	if n > len(s) {
		return -1
	}
	if n <= shortSub {
		// The matches that start in a chunk end before the chunk's end plus n-1.
		for i := 0; ; i += searchChunk {
			if i > 0 && poll != nil {
				poll()
			}
			end := min(len(s), i+searchChunk+n-1)
			if j := strings.Index(string(s[i:end]), string(sub)); j >= 0 {
				return i + j
			}
			if end == len(s) {
				return -1
			}
		}
	}

	// Candidates come from the first byte, as in strings.Index. Each failed candidate counts as
	// a comparison of all of sub, and once those exceed a linear budget, KMP takes over.
	c0, c1 := sub[0], sub[1]
	t := len(s) - n + 1
	pollFails := max(1, searchChunk/n)
	fails := 0
	for i := 0; i < t; {
		if s[i] != c0 {
			o := indexByte(s[i+1:t], c0, poll)
			if o < 0 {
				return -1
			}
			i += o + 1
		}
		if s[i+1] == c1 && s[i:i+n] == sub {
			return i
		}
		i++
		fails++
		if fails%pollFails == 0 && poll != nil {
			poll()
		}
		if fails > 16+4*(i/n) && i < t {
			if j := kmpIndex(s[i:], sub, false, poll); j >= 0 {
				return i + j
			}
			return -1
		}
	}
	return -1
}

// LastIndex returns the index of the last instance of sub in s, or -1, in time linear in the
// lengths of s and sub. poll is as for [Index].
func LastIndex(s, sub wtf8.String, poll func()) int {
	n := len(sub)
	if n > len(s) {
		return -1
	}
	if n <= shortSub {
		// The matches that end in a chunk start after the chunk's start minus n.
		for end := len(s); ; end -= searchChunk {
			if end < len(s) && poll != nil {
				poll()
			}
			start := max(0, end-searchChunk-n+1)
			if j := strings.LastIndex(string(s[start:end]), string(sub)); j >= 0 {
				return start + j
			}
			if start == 0 {
				return -1
			}
		}
	}

	// As in Index, from the last byte backwards.
	cl, c2 := sub[n-1], sub[n-2]
	pollFails := max(1, searchChunk/n)
	fails := 0
	// end is the end of the candidate.
	for end := len(s); end >= n; {
		if s[end-1] != cl {
			o := lastIndexByte(s[n-1:end-1], cl, poll)
			if o < 0 {
				return -1
			}
			end = n + o
		}
		if s[end-2] == c2 && s[end-n:end] == sub {
			return end - n
		}
		end--
		fails++
		if fails%pollFails == 0 && poll != nil {
			poll()
		}
		if fails > 16+4*((len(s)-end)/n) && end >= n {
			// The instance at r from the end covers the bytes from end-r-n to end-r.
			if r := kmpIndex(s[:end], sub, true, poll); r >= 0 {
				return end - r - n
			}
			return -1
		}
	}
	return -1
}

// indexByte is strings.IndexByte, calling poll between the chunks of a long s.
func indexByte(s wtf8.String, c byte, poll func()) int {
	for i := 0; i < len(s); i += searchChunk {
		if i > 0 && poll != nil {
			poll()
		}
		if j := strings.IndexByte(string(s[i:min(len(s), i+searchChunk)]), c); j >= 0 {
			return i + j
		}
	}
	return -1
}

// lastIndexByte is strings.LastIndexByte, calling poll between the chunks of a long s.
func lastIndexByte(s wtf8.String, c byte, poll func()) int {
	for end := len(s); end > 0; end -= searchChunk {
		if end < len(s) && poll != nil {
			poll()
		}
		start := max(0, end-searchChunk)
		if j := strings.LastIndexByte(string(s[start:end]), c); j >= 0 {
			return start + j
		}
	}
	return -1
}

// at returns the byte i of s, counted from the end with rev.
func at(s wtf8.String, i int, rev bool) byte {
	if rev {
		return s[len(s)-1-i]
	}
	return s[i]
}

// kmpIndex returns the index of the first instance of sub in s by the Knuth-Morris-Pratt
// algorithm, or -1. With rev, both s and sub are read from their ends, and the index is the
// number of bytes after the instance. poll is as for [Index].
func kmpIndex(s, sub wtf8.String, rev bool, poll func()) int {
	n := len(sub)
	// border[j] is the length of the longest proper border of the first j+1 bytes of sub.
	border := make([]int32, n)
	for j, k := 1, 0; j < n; j++ {
		if j%searchChunk == 0 && poll != nil {
			poll()
		}
		c := at(sub, j, rev)
		for k > 0 && at(sub, k, rev) != c {
			k = int(border[k-1])
		}
		if at(sub, k, rev) == c {
			k++
		}
		border[j] = int32(k)
	}
	for i, k := 0, 0; i < len(s); i++ {
		if i%searchChunk == 0 && i > 0 && poll != nil {
			poll()
		}
		c := at(s, i, rev)
		for k > 0 && at(sub, k, rev) != c {
			k = int(border[k-1])
		}
		if at(sub, k, rev) == c {
			k++
		}
		if k == n {
			return i - n + 1
		}
	}
	return -1
}
