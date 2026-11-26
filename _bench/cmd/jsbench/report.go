// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// engineTitles holds the names of the engines in the report.
var engineTitles = map[string]string{
	"ekuma":        "Ekuma",
	"goja":         "goja",
	"moejs":        "moejs",
	"sobek":        "Sobek",
	"otto":         "otto",
	"quickjs":      "modernc.org/quickjs",
	"qjs":          "QJS",
	"quickjs-c":    "QuickJS (C)",
	"v8go-jitless": "v8go --jitless",
	"v8go":         "v8go",
}

// A key identifies the runs of a benchmark on an engine.
type key struct {
	benchmark string
	engine    string
}

// A configLine is a configuration line of a results file.
type configLine struct {
	key   string
	value string
}

// results is the content of the results file of a suite.
type results struct {
	// config holds the configuration lines in order.
	config []configLine

	// benchmarks lists the names of the benchmarks in the order that they first appear.
	benchmarks []string

	// times holds the times of the runs in nanoseconds per iteration, in the order of the
	// rounds. A run without a result has NaN.
	times map[key][]float64

	// reasons holds why a run had no result, for the last such run.
	reasons map[key]string
}

var (
	configRe  = regexp.MustCompile(`^([a-z][^\s:]*): (.*)$`)
	resultRe  = regexp.MustCompile(`^Benchmark(\w+)/([^/\s]+)/([a-z0-9-]+?)(?:-\d+)?\s+\d+\s+(\S+) ns/op`)
	missingRe = regexp.MustCompile(`^--- (?:SKIP|FAIL): Benchmark(\w+)/([^/\s]+)/([a-z0-9-]+?): ?(.*)$`)
)

// parseResults parses the results file of the suite name.
func parseResults(name, text string) (*results, error) {
	r := &results{
		times:   map[key][]float64{},
		reasons: map[key]string{},
	}
	add := func(suite, benchmark, engine string, t float64) (key, error) {
		if suite != name {
			return key{}, fmt.Errorf("a result of the suite %s", suite)
		}
		k := key{
			benchmark: benchmark,
			engine:    engine,
		}
		if !slices.Contains(r.benchmarks, benchmark) {
			r.benchmarks = append(r.benchmarks, benchmark)
		}
		r.times[k] = append(r.times[k], t)
		return k, nil
	}
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\r\n")
		if m := configRe.FindStringSubmatch(line); m != nil {
			r.config = append(r.config, configLine{
				key:   m[1],
				value: m[2],
			})
			continue
		}
		if m := resultRe.FindStringSubmatch(line); m != nil {
			t, err := strconv.ParseFloat(m[4], 64)
			if err != nil {
				return nil, err
			}
			if _, err := add(m[1], m[2], m[3], t); err != nil {
				return nil, err
			}
			continue
		}
		if m := missingRe.FindStringSubmatch(line); m != nil {
			k, err := add(m[1], m[2], m[3], math.NaN())
			if err != nil {
				return nil, err
			}
			r.reasons[k] = m[4]
		}
	}
	return r, nil
}

// A cell is the summary of the runs of a benchmark on an engine.
type cell struct {
	// paired reports whether a round has the results of both the engine and the baseline.
	paired bool

	// ratio is the median over the paired rounds of the time relative to the baseline's.
	ratio float64

	// hasTime reports whether the engine has a result in any round.
	hasTime bool

	// time is the median time in nanoseconds per iteration.
	time float64
}

func (r *results) cell(benchmark, engine string) cell {
	base := r.times[key{
		benchmark: benchmark,
		engine:    comparison.Baseline,
	}]
	var times, ratios []float64
	for i, t := range r.times[key{
		benchmark: benchmark,
		engine:    engine,
	}] {
		if math.IsNaN(t) {
			continue
		}
		times = append(times, t)
		if i < len(base) && !math.IsNaN(base[i]) {
			ratios = append(ratios, t/base[i])
		}
	}
	var c cell
	if len(times) > 0 {
		c.hasTime = true
		c.time = median(times)
	}
	if len(ratios) > 0 {
		c.paired = true
		c.ratio = median(ratios)
	}
	return c
}

// geomean returns the geometric mean of engine's median ratios over benchmarks with paired
// results and the number of such benchmarks.
func (r *results) geomean(engine string) (float64, int) {
	var sum float64
	var n int
	for _, b := range r.benchmarks {
		if c := r.cell(b, engine); c.paired {
			sum += math.Log(c.ratio)
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return math.Exp(sum / float64(n)), n
}

// geomeanCell formats the geometric mean for engine, with a count when only some benchmarks
// have paired results.
func (r *results) geomeanCell(engine string) string {
	g, n := r.geomean(engine)
	if n == 0 {
		return "n/a"
	}
	if n < len(r.benchmarks) {
		return fmt.Sprintf("%s (%d/%d)", sig3(g), n, len(r.benchmarks))
	}
	return sig3(g)
}

// report reads the results files in dir and returns the sections of the report, keyed by name.
func report(dir string) (map[string]string, error) {
	var found []suite
	rs := map[string]*results{}
	for _, s := range suites {
		path := s.resultsPath(dir)
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		r, err := parseResults(s.name, string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		found = append(found, s)
		rs[s.name] = r
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no results files in %s", dir)
	}

	sections := map[string]string{}
	measuredEngines := map[string]bool{}
	for _, r := range rs {
		for k := range r.times {
			measuredEngines[k.engine] = true
		}
	}
	engines := slices.DeleteFunc(slices.Clone(comparison.Engines), func(e string) bool {
		return !measuredEngines[e]
	})

	// The summary has a row for each engine, with its geometric mean in each suite and its
	// version.
	var summary [][]string
	for _, e := range engines {
		row := []string{engineTitles[e]}
		var versions []string
		for _, s := range found {
			row = append(row, rs[s.name].geomeanCell(e))
			if v := rs[s.name].configValue(engineModules[e]); v != "" && !slices.Contains(versions, v) {
				versions = append(versions, v)
			}
		}
		summary = append(summary, append(row, strings.Join(versions, ", ")))
	}
	summaryHead := []string{""}
	for _, s := range found {
		summaryHead = append(summaryHead, s.title)
	}
	summaryHead = append(summaryHead, "Version")
	sections["summary"] = textTable("l"+strings.Repeat(".", len(found))+"l", summaryHead, summary) + "\n" + measuredOn(rs[found[0].name])

	var titles []string
	for _, e := range engines {
		titles = append(titles, engineTitles[e])
	}
	periods := strings.Repeat(".", len(engines))
	for _, s := range found {
		r := rs[s.name]
		var relative, times [][]string
		for _, b := range r.benchmarks {
			rel := []string{b}
			// The times of a benchmark share the unit that suits the time of the baseline.
			unit, size := timeUnit(r.cell(b, comparison.Baseline).time)
			t := []string{b, unit}
			for _, e := range engines {
				c := r.cell(b, e)
				if c.paired {
					rel = append(rel, sig3(c.ratio))
				} else {
					rel = append(rel, "n/a")
				}
				if c.hasTime {
					t = append(t, sig3(c.time/size))
				} else {
					t = append(t, "n/a")
				}
			}
			relative = append(relative, rel)
			times = append(times, t)
		}
		// The rows above show which benchmarks have paired results for each engine.
		geomean := []string{"Geomean"}
		for _, e := range engines {
			if g, n := r.geomean(e); n > 0 {
				geomean = append(geomean, sig3(g))
			} else {
				geomean = append(geomean, "n/a")
			}
		}
		relative = append(relative, geomean)
		sections[s.name+".relative"] = textTable("l"+periods, slices.Concat([]string{"Benchmark"}, titles), relative)
		sections[s.name+".time"] = textTable("ll"+periods, slices.Concat([]string{"Benchmark", "Unit"}, titles), times)
	}

	sections["environment"] = environmentTable(found, rs)
	sections["missing"] = missingList(found, rs, engines)
	return sections, nil
}

// configValue returns the value of the last configuration line of key, or an empty string if
// there is none.
func (r *results) configValue(key string) string {
	var v string
	for _, c := range r.config {
		if c.key == key {
			v = c.value
		}
	}
	return v
}

// measuredOn returns a sentence that tells when and on what r was measured.
func measuredOn(r *results) string {
	date, _, _ := strings.Cut(r.configValue("date"), "T")
	return fmt.Sprintf("Measured on %s: %s, %s, %s.\n", date, r.configValue("cpu"), r.configValue("os"), r.configValue("go"))
}

// environmentTable returns the table of the configuration lines of the results files of
// suites.
func environmentTable(suites []suite, rs map[string]*results) string {
	var keys []string
	for _, s := range suites {
		for _, c := range rs[s.name].config {
			if !slices.Contains(keys, c.key) {
				keys = append(keys, c.key)
			}
		}
	}
	var rows [][]string
	for _, k := range keys {
		row := []string{k}
		for _, s := range suites {
			row = append(row, strings.ReplaceAll(rs[s.name].configValue(k), "|", `\|`))
		}
		rows = append(rows, row)
	}
	head := []string{""}
	for _, s := range suites {
		head = append(head, s.title)
	}
	return table(head, rows, "---")
}

// missingList returns the list of the benchmarks that engines did not run, grouped by the
// reasons.
func missingList(suites []suite, rs map[string]*results, engines []string) string {
	var b strings.Builder
	for _, s := range suites {
		r := rs[s.name]
		for _, e := range engines {
			var reasons []string
			byReason := map[string][]string{}
			for _, bm := range r.benchmarks {
				k := key{
					benchmark: bm,
					engine:    e,
				}
				reason := r.reasons[k]
				runs := r.times[k]
				var missing int
				for _, t := range runs {
					if math.IsNaN(t) {
						missing++
					}
				}
				if missing == 0 && len(runs) > 0 {
					continue
				}
				if reason == "" {
					reason = "no result"
				}
				if missing > 0 && missing < len(runs) {
					reason = fmt.Sprintf("%d/%d rounds missing: %s", missing, len(runs), reason)
				}
				if _, ok := byReason[reason]; !ok {
					reasons = append(reasons, reason)
				}
				byReason[reason] = append(byReason[reason], bm)
			}
			for _, reason := range reasons {
				names := strings.Join(byReason[reason], ", ")
				if len(byReason[reason]) == len(r.benchmarks) {
					names = fmt.Sprintf("all %d", len(r.benchmarks))
				}
				fmt.Fprintf(&b, "- %s in %s (%s): `%s`\n", engineTitles[e], s.title, names, strings.ReplaceAll(reason, "`", "'"))
			}
		}
	}
	return b.String()
}

// table formats a Markdown table with the header head and rows. align is the alignment of the
// columns other than the first.
func table(head []string, rows [][]string, align string) string {
	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	writeRow(head)
	aligns := []string{"---"}
	for range head[1:] {
		aligns = append(aligns, align)
	}
	writeRow(aligns)
	for _, r := range rows {
		writeRow(r)
	}
	return b.String()
}

// textTable formats a table in a Markdown code block. aligns uses 'l' for left alignment and
// '.' to align decimal points and any count suffixes. A wide heading takes two lines.
func textTable(aligns string, head []string, rows [][]string) string {
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = slices.Clone(r)
	}
	for i := range head {
		if aligns[i] != '.' {
			continue
		}
		var column []string
		for _, r := range cells {
			column = append(column, r[i])
		}
		for j, c := range alignNumbers(column) {
			cells[j][i] = c
		}
	}

	widths := make([]int, len(head))
	for _, r := range cells {
		for i, c := range r {
			widths[i] = max(widths[i], utf8.RuneCountInString(c))
		}
	}
	top := make([]string, len(head))
	bottom := slices.Clone(head)
	var twoLines bool
	for i, h := range head {
		if utf8.RuneCountInString(h) > widths[i] {
			if top[i], bottom[i] = splitHeading(h); top[i] != "" {
				twoLines = true
			}
		}
		widths[i] = max(widths[i], utf8.RuneCountInString(top[i]), utf8.RuneCountInString(bottom[i]))
	}

	var b strings.Builder
	b.WriteString("```\n")
	writeRow := func(cells []string) {
		var line strings.Builder
		for i, c := range cells {
			if i > 0 {
				line.WriteString("  ")
			}
			line.WriteString(c + strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c)))
		}
		b.WriteString(strings.TrimRight(line.String(), " ") + "\n")
	}
	if twoLines {
		writeRow(top)
	}
	writeRow(bottom)
	for _, r := range cells {
		writeRow(r)
	}
	b.WriteString("```\n")
	return b.String()
}

// alignNumbers aligns decimal points and count suffixes in a column.
func alignNumbers(column []string) []string {
	var intWidth, fractionalWidth int
	for _, c := range column {
		value, _, _ := strings.Cut(c, " ")
		i := leadingDigits(value)
		intWidth = max(intWidth, i)
		fractionalWidth = max(fractionalWidth, utf8.RuneCountInString(value[i:]))
	}
	padded := make([]string, len(column))
	for j, c := range column {
		value, count, hasCount := strings.Cut(c, " ")
		i := leadingDigits(value)
		padded[j] = strings.Repeat(" ", intWidth-i) + value
		if hasCount {
			padded[j] += strings.Repeat(" ", fractionalWidth-utf8.RuneCountInString(value[i:])+1) + count
		}
	}
	return padded
}

// leadingDigits returns the number of the ASCII digits that s starts with.
func leadingDigits(s string) int {
	i := strings.IndexFunc(s, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if i < 0 {
		return len(s)
	}
	return i
}

// splitHeading splits the heading h into two lines at the space or after the slash that leaves
// the longer line shortest. The first line is empty if h has neither.
func splitHeading(h string) (string, string) {
	first, second := "", h
	for i, r := range h {
		var a, b string
		switch r {
		case ' ':
			a, b = h[:i], h[i+1:]
		case '/':
			a, b = h[:i+1], h[i+1:]
		default:
			continue
		}
		if max(utf8.RuneCountInString(a), utf8.RuneCountInString(b)) < max(utf8.RuneCountInString(first), utf8.RuneCountInString(second)) {
			first, second = a, b
		}
	}
	return first, second
}

// sectionNames returns the names of sections in the order of the report.
func sectionNames(sections map[string]string) []string {
	names := []string{"summary", "environment"}
	for _, s := range suites {
		names = append(names, s.name+".relative", s.name+".time")
	}
	names = append(names, "missing")
	return slices.DeleteFunc(names, func(name string) bool {
		_, ok := sections[name]
		return !ok
	})
}

// marked returns the section name with its content between its markers.
func marked(name, content string) string {
	return "<!-- BEGIN jsbench " + name + " -->\n\n" + content + "\n<!-- END jsbench " + name + " -->"
}

var beginRe = regexp.MustCompile(`<!-- BEGIN jsbench (\S+) -->`)

// update replaces the sections between markers in the file path with the ones of sections.
func update(path string, sections map[string]string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rest := string(b)
	var out strings.Builder
	var n int
	for {
		loc := beginRe.FindStringSubmatchIndex(rest)
		if loc == nil {
			break
		}
		name := rest[loc[2]:loc[3]]
		content, ok := sections[name]
		if !ok {
			return fmt.Errorf("%s: the report has no section %s", path, name)
		}
		end := "<!-- END jsbench " + name + " -->"
		i := strings.Index(rest[loc[1]:], end)
		if i < 0 {
			return fmt.Errorf("%s: the section %s has no end marker", path, name)
		}
		out.WriteString(rest[:loc[0]])
		out.WriteString(marked(name, content))
		rest = rest[loc[1]+i+len(end):]
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s: no sections to update", path)
	}
	out.WriteString(rest)
	return os.WriteFile(path, []byte(out.String()), 0o644)
}

func median(s []float64) float64 {
	s = slices.Sorted(slices.Values(s))
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// sig3 formats x, which is positive, with three significant digits.
func sig3(x float64) string {
	// The exponent of the rounded value decides the number of decimals.
	e := strconv.FormatFloat(x, 'e', 2, 64)
	exp, _ := strconv.Atoi(e[strings.IndexByte(e, 'e')+1:])
	return strconv.FormatFloat(x, 'f', max(2-exp, 0), 64)
}

// timeUnit returns the unit of time that suits the time ns in nanoseconds, and the size of
// the unit in nanoseconds.
func timeUnit(ns float64) (string, float64) {
	switch {
	case ns >= 1e9:
		return "s", 1e9
	case ns >= 1e6:
		return "ms", 1e6
	case ns >= 1e3:
		return "µs", 1e3
	}
	return "ns", 1
}
