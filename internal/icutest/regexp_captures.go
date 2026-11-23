// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"
)

// ICU's Extended driver takes the pattern and tagged input between matching delimiters.
func taggedCaptureRow(t *testing.T, row string) (pattern, flags, input string) {
	t.Helper()
	quoted := func(s string) (string, string) {
		t.Helper()
		if len(s) == 0 || !strings.ContainsRune(`"'/`, rune(s[0])) {
			t.Fatalf("missing quoted field in %q", row)
		}
		field, rest, ok := strings.Cut(s[1:], s[:1])
		if !ok {
			t.Fatalf("unterminated quoted field in %q", row)
		}
		return field, strings.TrimSpace(rest)
	}
	pattern, rest := quoted(row)
	i := strings.IndexAny(rest, `"'/`)
	if i < 0 {
		t.Fatalf("missing tagged input in %q", row)
	}
	input, _ = quoted(rest[i:])
	return pattern, strings.TrimSpace(rest[:i]), input
}

var captureTag = regexp.MustCompile(`^<(/?)([0-9]+)>`)
var numberedCaptureTag = regexp.MustCompile(`<[1-9][0-9]*>`)
var icuCodePointEscape = regexp.MustCompile(`\\U([[:xdigit:]]{8})|\\x\{([[:xdigit:]]+)\}`)

func containsRegexSyntax(pattern string, tokens ...string) bool {
	for _, token := range tokens {
		if strings.Contains(pattern, token) {
			return true
		}
	}
	return false
}

// taggedCaptureInput removes the fixture's tags and records their UTF-16 offsets.
// Captures inside lookarounds can lie outside the whole match.
func taggedCaptureInput(t *testing.T, tagged string) ([]any, string) {
	t.Helper()
	starts := map[int]int{}
	ends := map[int]int{}
	var offset, lastGroup int
	var points []any
	for len(tagged) > 0 {
		if tagged[0] == '<' {
			if tag := captureTag.FindStringSubmatch(tagged); tag != nil {
				group, err := strconv.Atoi(tag[2])
				if err != nil {
					t.Fatal(err)
				}
				if tag[1] == "/" {
					ends[group] = offset
				} else {
					starts[group] = offset
				}
				lastGroup = max(lastGroup, group)
				tagged = tagged[len(tag[0]):]
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(tagged)
		if r == '\\' {
			var err error
			if strings.HasPrefix(tagged, `\x{`) {
				hex, _, ok := strings.Cut(tagged[3:], "}")
				if !ok {
					t.Fatalf("unterminated hex escape in %q", tagged)
				}
				r, err = parseScalar(hex)
				size = len(hex) + 4
			} else {
				r, size, err = breakEscape(tagged, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		points = append(points, r)
		offset += max(1, utf16.RuneLen(r))
		tagged = tagged[size:]
	}
	var spans []string
	for group := 0; group <= lastGroup; group++ {
		start, opened := starts[group]
		end, closed := ends[group]
		switch {
		case opened != closed || end < start || group == 0 && !opened:
			t.Fatalf("invalid fixture span for capture %d", group)
		case opened:
			spans = append(spans, fmt.Sprintf("%d:%d", start, end))
		default:
			spans = append(spans, "-")
		}
	}
	return points, strings.Join(spans, " ")
}

func runTaggedCaptures(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	check := f.function(`(pattern, flags, points) => {
		const input = String.fromCodePoint(...points);
		const m = new RegExp(pattern, "du" + flags).exec(input);
		if (!m) throw new Error("expected a match");
		if (m.indices.length !== m.length || m.indices[0][0] !== m.index) {
			throw new Error("match and capture indices disagree");
		}
		const spans = m.indices.map((span, i) => {
			if (span === undefined && m[i] === undefined) return "-";
			if (span === undefined || m[i] !== input.slice(span[0], span[1])) {
				throw new Error("capture text disagrees with indices at group " + i);
			}
			return span.join(":");
		});
		// The fixture omits tags for groups that did not participate.
		while (spans.at(-1) === "-") spans.pop();
		return spans.join(" ");
	}`)
	fixtureLines(t, path, func(line int, row string) {
		row = strings.TrimSpace(row)
		if row == "" || strings.HasPrefix(row, "#") {
			return
		}
		pattern, flags, tagged := taggedCaptureRow(t, row)
		if !numberedCaptureTag.MatchString(tagged) && !(strings.Contains(tagged, "<0>") && icuCodePointEscape.MatchString(tagged)) {
			f.excluded["no tagged subgroup or tagged match with a code-point escape"]++
			return
		}
		if strings.Contains(tagged, "<r>") || strings.Trim(flags, "imsb") != "" {
			f.excluded["ICU matcher mode, region, or end-state assertion"]++
			return
		}
		// Transparent bounds have no effect when searching the entire input.
		flags = strings.ReplaceAll(flags, "b", "")
		switch {
		case containsRegexSyntax(pattern, "(?>", "}+", "*+"):
			f.excluded["atomic group or possessive quantifier"]++
			return
		case containsRegexSyntax(strings.ReplaceAll(pattern, `\\`, ""), `\A`, `\Z`, `\z`, `\040`):
			f.excluded["ICU anchor, quoting, or octal pattern syntax"]++
			return
		case containsRegexSyntax(pattern, "[:letter:]", "--", `\N{`, "{java", "{Is", "{In", "{all}"):
			f.excluded["ICU UnicodeSet, character name, or property syntax"]++
			return
		case line == 270, line == 301:
			f.excluded["ICU Unicode word boundary or digit class"]++
			return
		case line == 389, line == 392, line == 1220, line == 1325:
			f.excluded["ICU retains an optional empty repetition"]++
			return
		case line == 511:
			f.excluded["ICU backreference to a nonparticipating group fails"]++
			return
		case strings.Contains(flags, "i") && strings.Contains(pattern+tagged, "ß"):
			f.excluded["ICU full case folding"]++
			return
		}
		points, want := taggedCaptureInput(t, tagged)
		pattern = icuCodePointEscape.ReplaceAllString(pattern, `\u{${1}${2}}`)
		f.check(line, "RegExp.exec captures", check, want, pattern, flags, points)
	})
}

func runPerlCaptures(t *testing.T, path string) {
	f := newFormatFixture(t, path)
	check := f.function(`(pattern, flags, input, expression) => {
		const m = new RegExp(pattern, "d" + flags).exec(input);
		if (!m) throw new Error("expected a match");
		// ICU renders Perl's capture variables into the literal expected column.
		return expression.replace(/\\(.)|\$([+-])\[(\d+)\]|\$(\d+)|\$&|@[-+]/g,
			(token, escaped, side, group, number) => {
				if (escaped !== undefined) return escaped === "n" ? "\n" : escaped;
				if (side !== undefined) return m.indices[group]?.[side === "+" ? 1 : 0] ?? "";
				if (number !== undefined) return m[number] ?? "";
				if (token === "$&") return m[0];
				return m.indices.map(span => span?.[token === "@+" ? 1 : 0] ?? -1).join(" ");
			});
	}`)
	inputEscapes := strings.NewReplacer(`\n`, "\n", "${nulnul}", "\x00\x00", "${ffff}", "\uffff")
	fixtureLines(t, path, func(line int, row string) {
		fields := strings.Split(row, "\t")
		if len(fields) < 4 {
			t.Fatalf("%s:%d: missing Perl fixture columns", path, line)
		}
		pattern, input, outcome, expression := fields[0], fields[1], fields[2], fields[3]
		if !strings.Contains(outcome, "y") || strings.Contains(outcome, "i") || expression == "-" {
			f.excluded["no successful capture oracle or marked incompatible by ICU"]++
			return
		}
		var flags string
		if strings.HasPrefix(pattern, "'") {
			end := strings.LastIndexByte(pattern, '\'')
			if end == 0 {
				t.Fatalf("%s:%d: unterminated Perl pattern", path, line)
			}
			pattern, flags = pattern[1:end], pattern[end+1:]
		}
		switch {
		case strings.Contains(flags, "x"), containsRegexSyntax(pattern, "(?x", "(?-x", "(?#", "(?{", "(?(", "(?>"):
			f.excluded["Perl extended syntax, atomic group, conditional, or code expression"]++
			return
		case containsRegexSyntax(pattern, "(?i)", "(?-i)", "(?s)", "(?m)"):
			f.excluded["unscoped inline mode modifier"]++
			return
		case containsRegexSyntax(pattern, `\Z`, `\z`), strings.Contains(pattern, "$") && strings.HasSuffix(input, `\n`) && !strings.Contains(flags, "m") && !strings.Contains(pattern, "(?m:"):
			f.excluded["Perl end anchor or final newline behavior"]++
			return
		case containsRegexSyntax(pattern, "[:", "[]]", "[^]"):
			f.excluded["Perl bracket expression"]++
			return
		}
		switch line {
		case 256, 257, 407, 419, 428, 865:
			// ECMAScript clears inner captures at each repetition.
			f.excluded["capture or backreference from a previous repetition"]++
			return
		}
		pattern = strings.ReplaceAll(pattern, "${bang}", "!")
		var want string
		if len(fields) >= 5 {
			want = inputEscapes.Replace(fields[4])
		}
		f.check(line, "RegExp.exec capture expression", check, want, pattern, flags, inputEscapes.Replace(input), expression)
	})
}
