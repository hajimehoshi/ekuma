// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"bufio"
	"cmp"
	"os"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

func runLikelySubtagsTest(t *testing.T, path string, skips localeSkips) {
	var vm ekuma.VM
	maximize := newLocaleOperation(t, &vm, path, "maximize", `tag => new Intl.Locale(tag).maximize().toString()`, skips)
	minimize := newLocaleOperation(t, &vm, path, "minimize", `tag => new Intl.Locale(tag).minimize().toString()`, skips)
	forEachLocaleRow(t, path, 4, func(line int, fields []string) {
		source, add, favorScript, favorRegion := fields[0], fields[1], fields[2], fields[3]
		// ECMA-402 returns the input locale when a likely-subtags operation fails.
		if add == "FAIL" {
			add = source
		}
		// An omitted removal result inherits the preceding column.
		remove := cmp.Or(favorRegion, favorScript, add)
		maximize.check(t, line, source, add)
		minimize.check(t, line, source, remove)
	})
}

func runLocaleCanonicalizationTest(t *testing.T, path string, skips localeSkips) {
	var vm ekuma.VM
	canonicalize := newLocaleOperation(t, &vm, path, "Intl.getCanonicalLocales", `tag => Intl.getCanonicalLocales(tag).join(",")`, skips)
	forEachLocaleRow(t, path, 2, func(line int, fields []string) {
		// The fixture uses the CLDR subtag separator on both sides of the alias.
		source := strings.ReplaceAll(fields[0], "_", "-")
		want := strings.ReplaceAll(fields[1], "_", "-")
		canonicalize.check(t, line, source, want)
	})
}

type localeSkips map[string]map[string]string // operation to input tag to known result

func readLocaleSkips(t *testing.T) localeSkips {
	t.Helper()
	const path = "internal/icutest/testdata/icu-skips.txt"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	skips := localeSkips{
		"maximize":                 {},
		"minimize":                 {},
		"Intl.getCanonicalLocales": {},
	}
	for i, row := range strings.Split(string(data), "\n") {
		row, _, _ = strings.Cut(row, "#")
		fields := strings.Fields(row)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 || skips[fields[0]] == nil {
			t.Fatalf("%s:%d: expected a locale operation, input tag and known result, got %q", path, i+1, row)
		}
		entries := skips[fields[0]]
		if _, ok := entries[fields[1]]; ok {
			t.Fatalf("%s:%d: duplicate entry for %s(%q)", path, i+1, fields[0], fields[1])
		}
		entries[fields[1]] = fields[2]
	}
	return skips
}

type localeOperation struct {
	path          string
	name          string
	function      ekuma.Value
	remaining     map[string]string
	knownFailures int
}

func newLocaleOperation(t *testing.T, vm *ekuma.VM, path, name, source string, skips localeSkips) *localeOperation {
	t.Helper()
	script, err := vm.ParseScript("locale-test.js", source)
	if err != nil {
		t.Fatal(err)
	}
	f, err := script.Evaluate(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	op := &localeOperation{
		path:      path,
		name:      name,
		function:  f,
		remaining: skips[name],
	}
	t.Cleanup(func() {
		for input := range op.remaining {
			t.Errorf("%s: icu-skips.txt entry for %s(%q) matches no case", path, name, input)
		}
		t.Logf("%s: %d known failures", name, op.knownFailures)
	})
	return op
}

func (op *localeOperation) check(t *testing.T, line int, source, want string) {
	t.Helper()
	known, listed := op.remaining[source]
	delete(op.remaining, source)
	value, err := op.function.Invoke(t.Context(), source)
	if err != nil {
		t.Errorf("%s:%d: %s(%q): got error %v, want %q", op.path, line, op.name, source, err, want)
		return
	}
	got := value.String()
	switch {
	case listed && got == want:
		t.Errorf("%s:%d: %s(%q): got expected %q; remove the icu-skips.txt entry", op.path, line, op.name, source, got)
	case listed && got == known:
		op.knownFailures++
	case got != want:
		t.Errorf("%s:%d: %s(%q): got %q, want %q", op.path, line, op.name, source, got, want)
	}
}

func forEachLocaleRow(t *testing.T, path string, columns int, test func(line int, fields []string)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var rows, line int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line++
		data, _, _ := strings.Cut(scanner.Text(), "#")
		if strings.TrimSpace(data) == "" {
			continue
		}
		fields := strings.Split(data, ";")
		if len(fields) != columns {
			t.Fatalf("%s:%d: got %d columns, want %d", path, line, len(fields), columns)
		}
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if fields[0] == "" || fields[1] == "" {
			t.Fatalf("%s:%d: missing source or expected result", path, line)
		}
		test(line, fields)
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Fatalf("%s contains no locale tests", path)
	}
	t.Logf("%d rows executed, 0 excluded", rows)
}
