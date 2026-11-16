// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hajimehoshi/ekuma/_bench/internal/comparison"
)

// engineTitles holds the names of the engines in the report.
var engineTitles = map[string]string{
	"ekuma":        "Ekuma",
	"goja":         "goja",
	"sobek":        "Sobek",
	"otto":         "otto",
	"quickjs":      "modernc.org/quickjs",
	"qjs":          "QJS",
	"quickjs-c":    "QuickJS (C)",
	"v8go-jitless": "v8go `--jitless`",
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
	configRe  = regexp.MustCompile(`^([a-z][^\sA-Z:]*): (.*)$`)
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
	// ok reports whether a round has the results of both the engine and the baseline.
	ok bool

	// ratio is the median over the rounds of the time relative to the baseline's in the same
	// round.
	ratio float64

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
	if len(ratios) == 0 {
		return cell{}
	}
	return cell{
		ok:    true,
		ratio: median(ratios),
		time:  median(times),
	}
}

// geomean returns the geometric mean of the ratios of engine over the benchmarks that it ran,
// and the number of these benchmarks.
func (r *results) geomean(engine string) (float64, int) {
	var sum float64
	var n int
	for _, b := range r.benchmarks {
		if c := r.cell(b, engine); c.ok {
			sum += math.Log(c.ratio)
			n++
		}
	}
	if n == 0 {
		return 0, 0
	}
	return math.Exp(sum / float64(n)), n
}

// geomeanCell formats the geometric mean of engine, with the number of the benchmarks that it
// ran if it did not run all of them.
func (r *results) geomeanCell(engine string) string {
	g, n := r.geomean(engine)
	if n == 0 {
		return "n/a"
	}
	if n < len(r.benchmarks) {
		return fmt.Sprintf("%s (%d of %d)", sig3(g), n, len(r.benchmarks))
	}
	return sig3(g)
}

// report reads the results files in dir and returns the sections of the report, keyed by name.
func report(dir string) (map[string]string, error) {
	var found []suite
	rs := map[string]*results{}
	for _, s := range suites {
		b, err := os.ReadFile(filepath.Join(dir, s.name+".txt"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		r, err := parseResults(s.name, string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Join(dir, s.name+".txt"), err)
		}
		found = append(found, s)
		rs[s.name] = r
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("no results files in %s", dir)
	}

	var heads []string
	for _, e := range comparison.Engines {
		heads = append(heads, engineTitles[e])
	}
	sections := map[string]string{}

	var summary [][]string
	for _, s := range found {
		row := []string{s.title}
		for _, e := range comparison.Engines {
			row = append(row, rs[s.name].geomeanCell(e))
		}
		summary = append(summary, row)
	}
	sections["summary"] = table(slices.Concat([]string{""}, heads), summary, "--:") + "\n" + measuredOn(rs[found[0].name])

	for _, s := range found {
		r := rs[s.name]
		var relative, times [][]string
		for _, b := range r.benchmarks {
			rel := []string{b}
			t := []string{b}
			for _, e := range comparison.Engines {
				c := r.cell(b, e)
				if !c.ok {
					rel = append(rel, "n/a")
					t = append(t, "n/a")
					continue
				}
				rel = append(rel, sig3(c.ratio))
				t = append(t, formatTime(c.time))
			}
			relative = append(relative, rel)
			times = append(times, t)
		}
		geomean := []string{"Geomean"}
		for _, e := range comparison.Engines {
			geomean = append(geomean, r.geomeanCell(e))
		}
		relative = append(relative, geomean)
		sections[s.name+".relative"] = table(slices.Concat([]string{"Benchmark"}, heads), relative, "--:")
		sections[s.name+".time"] = table(slices.Concat([]string{"Benchmark"}, heads), times, "--:")
	}

	sections["environment"] = environmentTable(found, rs)
	sections["missing"] = missingList(found, rs)
	return sections, nil
}

// measuredOn returns a sentence that tells when and on what r was measured.
func measuredOn(r *results) string {
	config := map[string]string{}
	for _, c := range r.config {
		config[c.key] = c.value
	}
	date, _, _ := strings.Cut(config["date"], "T")
	return fmt.Sprintf("Measured on %s: %s, %s, %s.\n", date, config["cpu"], config["os"], config["go"])
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
			var v string
			for _, c := range rs[s.name].config {
				if c.key == k {
					v = c.value
				}
			}
			row = append(row, strings.ReplaceAll(v, "|", `\|`))
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
func missingList(suites []suite, rs map[string]*results) string {
	var b strings.Builder
	for _, s := range suites {
		r := rs[s.name]
		for _, e := range comparison.Engines {
			var reasons []string
			byReason := map[string][]string{}
			for _, bm := range r.benchmarks {
				if r.cell(bm, e).ok {
					continue
				}
				reason := r.reasons[key{
					benchmark: bm,
					engine:    e,
				}]
				if reason == "" {
					reason = "no result"
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

// formatTime formats the time ns in nanoseconds with three significant digits and a unit.
func formatTime(ns float64) string {
	rounded, _ := strconv.ParseFloat(strconv.FormatFloat(ns, 'e', 2, 64), 64)
	switch {
	case rounded >= 1e9:
		return sig3(ns/1e9) + " s"
	case rounded >= 1e6:
		return sig3(ns/1e6) + " ms"
	case rounded >= 1e3:
		return sig3(ns/1e3) + " µs"
	}
	return sig3(ns) + " ns"
}
