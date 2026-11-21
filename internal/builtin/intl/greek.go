// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/hajimehoshi/ekuma/internal/xstrings"
)

type greekMarkForm uint8

// A precomposed mark takes precedence when both forms occur on a letter.
const (
	noMark greekMarkForm = iota
	combiningMark
	precomposedMark
)

type greekDiacritics struct {
	accent         greekMarkForm
	diaeresis      greekMarkForm
	iotaSubscripts int
}

func (d *greekDiacritics) add(r rune, form greekMarkForm) bool {
	switch r {
	case '\u0300', '\u0301', '\u0302', '\u0303', '\u0311', '\u0342':
		d.accent = max(d.accent, form)
	case '\u0308':
		d.diaeresis = max(d.diaeresis, form)
	case '\u0344': // Combining dialytika tonos.
		d.accent = max(d.accent, form)
		d.diaeresis = max(d.diaeresis, form)
	case '\u0345':
		d.iotaSubscripts++
	case '\u0304', '\u0306', '\u0313', '\u0314', '\u0343':
		// Length and breathing marks drop out in uppercase.
	default:
		return false
	}
	return true
}

func (d greekDiacritics) writeUpper(b *strings.Builder, upper rune, keepAccent bool, poll func()) {
	if keepAccent && d.accent == precomposedMark {
		upper = 'Ή'
	}
	if d.diaeresis == precomposedMark {
		switch upper {
		case 'Ι':
			upper = 'Ϊ'
			d.diaeresis = noMark
		case 'Υ':
			upper = 'Ϋ'
			d.diaeresis = noMark
		}
	}
	b.WriteRune(upper)
	if d.diaeresis != noMark {
		b.WriteRune('\u0308')
	}
	if keepAccent && d.accent == combiningMark {
		b.WriteRune('\u0301')
	}
	for n := d.iotaSubscripts; n > 0; n-- {
		if n%pollRunes == 0 {
			poll()
		}
		b.WriteRune('Ι')
	}
}

func isGreekCaseLetter(r rune) bool {
	// Greek and Coptic, Greek Extended, and the canonically equivalent ohm sign.
	if !(r >= 0x0370 && r <= 0x03ff || r >= 0x1f00 && r <= 0x1fff || r == 'Ω') {
		return false
	}
	return unicode.IsLetter(r) && unicode.Is(unicode.Greek, r)
}

// greekUpper applies Greek tailoring before default uppercasing.
func greekUpper(s string, poll func()) string {
	var b strings.Builder
	var previousAccent greekMarkForm
	var copied int
	for i, n := 0, 0; i < len(s); n++ {
		if n%pollRunes == 0 {
			poll()
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isGreekCaseLetter(r) {
			previousAccent = noMark
			i += size
			continue
		}
		var marks greekDiacritics
		if d := norm.NFD.PropertiesString(s[i:]).Decomposition(); len(d) != 0 {
			var baseSize int
			r, baseSize = utf8.DecodeRune(d)
			for _, mark := range string(d[baseSize:]) {
				marks.add(mark, precomposedMark)
			}
		}
		end := i + size
		for _, mark := range s[end:] {
			if !marks.add(mark, combiningMark) {
				break
			}
			end += utf8.RuneLen(mark)
			if n++; n%pollRunes == 0 {
				poll()
			}
		}

		upper := unicode.ToUpper(r)
		if upper == 'Ι' || upper == 'Υ' {
			// A removed vowel accent marks the hiatus on the following vowel.
			marks.diaeresis = max(marks.diaeresis, previousAccent)
		}
		previousAccent = noMark
		// A precomposed Ϊ or Ϋ ends the transfer even if its vowel had an accent.
		if strings.ContainsRune("ΑΕΗΙΟΥΩ", upper) && marks.diaeresis != precomposedMark {
			previousAccent = marks.accent
		}
		// The disjunctive ή ("or") keeps its accent when it stands alone.
		keepAccent := upper == 'Η' && marks.accent != noMark && marks.iotaSubscripts == 0 &&
			!xstrings.EndsWithCasedLetter(s[:i], poll) && !xstrings.StartsWithCasedLetter(s[end:], poll)
		b.WriteString(s[copied:i])
		marks.writeUpper(&b, upper, keepAccent, poll)
		copied = end
		i = end
	}
	if copied == 0 {
		return s
	}
	b.WriteString(s[copied:])
	return b.String()
}
