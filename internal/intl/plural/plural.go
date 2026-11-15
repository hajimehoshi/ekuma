// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// Package plural implements the plural rules of the Unicode Common Locale Data Repository.
package plural

import (
	"strconv"
	"strings"
	"sync"

	"github.com/hajimehoshi/ekuma/internal/intl/cldr"
	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

// categoryOrder are the plural categories in the order of ECMA-402.
var categoryOrder = []string{"zero", "one", "two", "few", "many", "other"}

// lookup returns the data of m for the locale l of CLDR: that of l, one of its ancestors
// other than the root locale, its language, or the root locale.
func lookup(m cldr.Map, l string) (string, bool) {
	for p, ok := l, true; ok && p != "und"; p, ok = locale.Parent(p) {
		if v, ok := m.Lookup(p); ok {
			return v, true
		}
	}
	lang, _, _ := strings.Cut(l, "-")
	if v, ok := m.Lookup(lang); ok {
		return v, true
	}
	return m.Lookup("und")
}

// rule is the condition of a plural category.
type rule struct {
	category string
	cond     orCondition
}

type orCondition []andCondition

type andCondition []relation

type relation struct {
	operand byte
	mod     int64
	negate  bool
	ranges  [][2]int64
}

type ruleKey struct {
	locale  string
	ordinal bool
}

var ruleCache sync.Map

// rules returns the rules of the locale l of CLDR.
func rules(l string, ordinal bool) []rule {
	k := ruleKey{
		locale:  l,
		ordinal: ordinal,
	}
	if v, ok := ruleCache.Load(k); ok {
		return v.([]rule)
	}
	m := cldr.PluralRules
	if ordinal {
		m = cldr.OrdinalRules
	}
	var rs []rule
	if s, ok := lookup(m, l); ok && s != "" {
		for r := range strings.SplitSeq(s, ";") {
			c, cond, _ := strings.Cut(r, ":")
			rs = append(rs, rule{
				category: c,
				cond:     parseCondition(cond),
			})
		}
	}
	ruleCache.Store(k, rs)
	return rs
}

// parseCondition parses a condition of the plural rule syntax of UTS 35.
func parseCondition(s string) orCondition {
	var or orCondition
	for a := range strings.SplitSeq(s, " or ") {
		var and andCondition
		for r := range strings.SplitSeq(a, " and ") {
			and = append(and, parseRelation(strings.TrimSpace(r)))
		}
		or = append(or, and)
	}
	return or
}

func parseRelation(s string) relation {
	var r relation
	op, list, ok := strings.Cut(s, "!=")
	if ok {
		r.negate = true
	} else {
		op, list, _ = strings.Cut(s, "=")
	}
	op = strings.TrimSpace(op)
	r.operand = op[0]
	if _, m, ok := strings.Cut(op, "%"); ok {
		r.mod, _ = strconv.ParseInt(strings.TrimSpace(m), 10, 64)
	}
	for item := range strings.SplitSeq(strings.TrimSpace(list), ",") {
		lo, hi, ok := strings.Cut(strings.TrimSpace(item), "..")
		if !ok {
			hi = lo
		}
		l, _ := strconv.ParseInt(lo, 10, 64)
		h, _ := strconv.ParseInt(hi, 10, 64)
		r.ranges = append(r.ranges, [2]int64{l, h})
	}
	return r
}

// operands are the operands of a number for plural rules.
type operands struct {
	// i are the integer digits.
	i string
	// f are the visible fraction digits.
	f string
	// e is the exponent of compact decimal notation.
	e int
}

func newOperands(s string, e int) operands {
	i, f, _ := strings.Cut(s, ".")
	i = strings.TrimLeft(i, "0")
	if i == "" {
		i = "0"
	}
	return operands{
		i: i,
		f: f,
		e: e,
	}
}

// value returns the value of the operand op as decimal digits, and reports whether the value
// has a fraction that is not zero.
func (o operands) value(op byte) (string, bool) {
	switch op {
	case 'n':
		return o.i, strings.Trim(o.f, "0") != ""
	case 'i':
		return o.i, false
	case 'v':
		return strconv.Itoa(len(o.f)), false
	case 'w':
		return strconv.Itoa(len(strings.TrimRight(o.f, "0"))), false
	case 'f':
		return o.f, false
	case 't':
		return strings.TrimRight(o.f, "0"), false
	case 'c', 'e':
		return strconv.Itoa(o.e), false
	}
	return "0", false
}

func (r relation) matches(o operands) bool {
	digits, frac := o.value(r.operand)
	var v int64
	huge := false
	if r.mod > 0 {
		for i := range len(digits) {
			v = (v*10 + int64(digits[i]-'0')) % r.mod
		}
	} else {
		digits = strings.TrimLeft(digits, "0")
		if len(digits) > 18 {
			huge = true
		} else {
			v, _ = strconv.ParseInt("0"+digits, 10, 64)
		}
	}
	in := false
	if !frac && !huge {
		for _, rg := range r.ranges {
			if rg[0] <= v && v <= rg[1] {
				in = true
				break
			}
		}
	}
	return in != r.negate
}

func (c orCondition) matches(o operands) bool {
	for _, and := range c {
		ok := true
		for _, r := range and {
			if !r.matches(o) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Select returns the plural category of the number s in the locale l of CLDR. s is a
// non-negative decimal string of ASCII digits with an optional full stop, and e is the
// exponent of compact decimal notation that s was formatted with, 0 for other notations.
// ordinal selects the ordinal rules instead of the cardinal rules.
func Select(l string, ordinal bool, s string, e int) string {
	o := newOperands(s, e)
	for _, r := range rules(l, ordinal) {
		if r.cond.matches(o) {
			return r.category
		}
	}
	return "other"
}

// Categories returns the plural categories of the locale l of CLDR in the order "zero",
// "one", "two", "few", "many", "other".
func Categories(l string, ordinal bool) []string {
	rs := rules(l, ordinal)
	var out []string
	for _, c := range categoryOrder {
		if c == "other" {
			out = append(out, c)
			continue
		}
		for _, r := range rs {
			if r.category == c {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// SelectRange returns the plural category of a range from a number of the category start
// to one of the category end in the locale l of CLDR.
func SelectRange(l, start, end string) string {
	s, ok := lookup(cldr.PluralRanges, l)
	if !ok {
		return "other"
	}
	prefix := start + " " + end + " "
	for r := range strings.SplitSeq(s, ";") {
		if v, ok := strings.CutPrefix(r, prefix); ok {
			return v
		}
	}
	return "other"
}
