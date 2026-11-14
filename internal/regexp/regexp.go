// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package regexp implements ECMAScript regular expressions.
//
// Patterns and subjects are JavaScript strings held as Go strings in WTF-8
// (see package wtf8). Every index that the package takes or returns is a UTF-16 code unit
// index, so the results are those that the specification describes for UTF-16 strings.
package regexp

import (
	"errors"
	"strings"
)

// Flags is a set of regular expression flags.
type Flags uint16

const (
	FlagHasIndices  Flags = 1 << iota // d
	FlagGlobal                        // g
	FlagIgnoreCase                    // i
	FlagMultiline                     // m
	FlagDotAll                        // s
	FlagUnicode                       // u
	FlagUnicodeSets                   // v
	FlagSticky                        // y
)

// flagLetters lists the flag letters in the order of the bits of [Flags].
const flagLetters = "dgimsuvy"

// ErrSyntax is the error that every syntax error of a pattern or of flags matches with [errors.Is].
var ErrSyntax = errors.New("regexp: syntax error")

// Error is a syntax error in a pattern or in flags.
type Error struct {
	Msg string
}

func (e *Error) Error() string {
	return "invalid regular expression: " + e.Msg
}

// Is reports whether target is [ErrSyntax].
func (e *Error) Is(target error) bool {
	return target == ErrSyntax
}

// ParseFlags parses the flags of a regular expression literal or of the RegExp constructor.
func ParseFlags(s string) (Flags, error) {
	var f Flags
	for i := 0; i < len(s); i++ {
		j := strings.IndexByte(flagLetters, s[i])
		if j < 0 || f&(1<<j) != 0 {
			return 0, &Error{Msg: "invalid flags '" + s + "'"}
		}
		f |= 1 << j
	}
	if f&FlagUnicode != 0 && f&FlagUnicodeSets != 0 {
		return 0, &Error{Msg: "invalid flags '" + s + "'"}
	}
	return f, nil
}

// String returns the flags as letters in the canonical order.
func (f Flags) String() string {
	var b [len(flagLetters)]byte
	n := 0
	for i := range len(flagLetters) {
		if f&(1<<i) != 0 {
			b[n] = flagLetters[i]
			n++
		}
	}
	return string(b[:n])
}

// Regexp is a compiled regular expression. It is immutable and may be used by several
// goroutines at once, each with its own [Matcher].
type Regexp struct {
	prog  *program
	flags Flags

	// groupCount is the number of capturing groups, without the whole match.
	groupCount int

	// groupNames maps each capturing group index from 1 to its name, or "".
	groupNames []string

	// names lists the distinct group names in the order of their first group.
	names []string
}

// Compile parses a pattern with flags and compiles it.
func Compile(pattern string, flags Flags) (*Regexp, error) {
	tree, err := parse(pattern, flags)
	if err != nil {
		return nil, err
	}
	re := &Regexp{
		flags:      flags,
		groupCount: tree.groupCount,
		groupNames: tree.groupNames,
		names:      tree.names,
	}
	re.prog = compile(tree, flags)
	return re, nil
}

// Validate reports the syntax error of pattern with the flags in flags, if any.
func Validate(pattern, flags string) error {
	f, err := ParseFlags(flags)
	if err != nil {
		return err
	}
	_, err = parse(pattern, f)
	return err
}

// Flags returns the flags that re was compiled with.
func (re *Regexp) Flags() Flags {
	return re.flags
}

// GroupCount returns the number of capturing groups of re, not counting the whole match.
func (re *Regexp) GroupCount() int {
	return re.groupCount
}

// GroupName returns the name of the capturing group i, which is from 1 to [Regexp.GroupCount], or
// "".
func (re *Regexp) GroupName(i int) string {
	return re.groupNames[i]
}

// Names returns the distinct group names of re in the order of their first appearance.
// The caller must not modify the result.
func (re *Regexp) Names() []string {
	return re.names
}

// Poller is what a long match calls now and then, so that it can be stopped.
// [Poller.Poll] stops the match by panicking.
type Poller interface {
	Poll()
}
