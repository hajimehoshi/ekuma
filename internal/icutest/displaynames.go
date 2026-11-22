// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"strings"
	"testing"
)

func runDisplayNames(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	of := f.function(`(locale, languageDisplay, code) => new Intl.DisplayNames(locale, {type:"language", languageDisplay}).of(code)`)
	var locale, mode string
	fixtureLines(t, path, func(line int, row string) {
		switch {
		case row == "" || strings.HasPrefix(row, "#"):
			return
		case strings.HasPrefix(row, "@locale="):
			locale = strings.ReplaceAll(strings.TrimPrefix(row, "@locale="), "_", "-")
			return
		case strings.HasPrefix(row, "@languageDisplay="):
			mode = strings.TrimPrefix(row, "@languageDisplay=")
			return
		}
		input, want, ok := strings.Cut(row, "; ")
		if !ok || locale == "" || (mode != "dialect" && mode != "standard") {
			t.Fatalf("%s:%d: invalid display-name case %q", path, line, row)
		}
		switch {
		case locale == "root":
			f.excluded["ICU root display locale cannot be selected by Intl locale negotiation"]++
		case strings.Contains(input, "-u-") || strings.Contains(input, "-t-"):
			f.excluded["Intl.DisplayNames language codes exclude locale extensions"]++
		default:
			f.check(line, "Intl.DisplayNames.of", of, want, locale, mode, input)
		}
	})
}
