// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"bufio"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

type formatFixture struct {
	t               *testing.T
	path            string
	executed, known int
	excluded        map[string]int
	failures        map[string]map[int]string
}

func newFormatFixture(t *testing.T, path string) *formatFixture {
	f := &formatFixture{
		t:        t,
		path:     path,
		excluded: map[string]int{},
		failures: map[string]map[int]string{},
	}
	t.Cleanup(func() {
		t.Logf("%d cases executed, %d known failures", f.executed, f.known)
		for _, reason := range slices.Sorted(maps.Keys(f.excluded)) {
			t.Logf("%d excluded: %s", f.excluded[reason], reason)
		}
		if f.executed == 0 {
			t.Errorf("%s: no cases executed", path)
		}
	})
	return f
}

func (f *formatFixture) trackOperations(failures map[string]map[int]string) {
	prefix := filepath.Base(f.path) + "/"
	for key, entries := range failures {
		if op, ok := strings.CutPrefix(key, prefix); ok {
			f.track(op, entries)
		}
	}
}

func (f *formatFixture) track(op string, entries map[int]string) {
	f.failures[op] = entries
	f.t.Cleanup(func() {
		for line := range entries {
			f.t.Errorf("%s:%d: %s entry for %s matches no case", f.path, line, failuresFile, op)
		}
	})
}

func (f *formatFixture) function(source string) ekuma.Value {
	f.t.Helper()
	var vm ekuma.VM
	return f.functionIn(&vm, source)
}

func (f *formatFixture) functionIn(vm *ekuma.VM, source string) ekuma.Value {
	f.t.Helper()
	s, err := vm.ParseScript(f.path+".js", source)
	if err != nil {
		f.t.Fatal(err)
	}
	fn, err := s.Evaluate(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return fn
}

func (f *formatFixture) check(line int, op string, fn ekuma.Value, want string, args ...any) {
	f.t.Helper()
	got, err := fn.Invoke(f.t.Context(), args...)
	if err != nil {
		f.executed++
		f.t.Errorf("%s:%d: %s(%v): got error %v, want %q", f.path, line, op, args, err, want)
		return
	}
	f.checkValue(line, op, got.String(), want, args...)
}

func (f *formatFixture) checkValue(line int, op, actual, want string, args ...any) {
	f.t.Helper()
	f.executed++
	known, listed := f.failures[op][line]
	delete(f.failures[op], line)
	if known == "web-spaces" {
		known = strings.ReplaceAll(want, "\u202f", " ")
	}
	switch {
	case listed && actual == want:
		f.t.Errorf("%s:%d: %s(%v): got expected %q; remove the %s entry", f.path, line, op, args, want, failuresFile)
	case listed && actual == known:
		f.known++
	case actual != want:
		if op == "formatRangeToParts" {
			gotParts, wantParts := strings.Split(actual, "\n"), strings.Split(want, "\n")
			for i := range min(len(gotParts), len(wantParts)) {
				if gotParts[i] != wantParts[i] {
					f.t.Errorf("%s:%d: %s(%v): part %d: got %q, want %q", f.path, line, op, args, i, gotParts[i], wantParts[i])
					return
				}
			}
			f.t.Errorf("%s:%d: %s(%v): got %d parts, want %d", f.path, line, op, args, len(gotParts), len(wantParts))
			return
		}
		f.t.Errorf("%s:%d: %s(%v): got %q, want %q", f.path, line, op, args, actual, want)
	}
}

func fixtureLines(t *testing.T, path string, visit func(int, string)) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for line := 1; s.Scan(); line++ {
		row := s.Text()
		if line == 1 {
			row = strings.TrimPrefix(row, "\uFEFF")
		}
		visit(line, row)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}

// resource represents an ICU text resource with its original source position.
type resource struct {
	name     string
	line     int
	values   []string
	children []resource
}

func (r resource) child(name string) resource {
	for _, c := range r.children {
		if c.name == name {
			return c
		}
	}
	return resource{}
}

func readResource(t *testing.T, path string) resource {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.TrimPrefix(string(data), "\uFEFF")
	line := 1
	var next string
	var quoted bool
	advance := func() {
		for {
			n := len(source) - len(strings.TrimLeft(source, " \r\n\t"))
			line += strings.Count(source[:n], "\n")
			source = source[n:]
			if !strings.HasPrefix(source, "//") {
				break
			}
			_, source, _ = strings.Cut(source, "\n")
			line++
		}
		next = ""
		quoted = false
		if source == "" {
			return
		}
		if source[0] == '"' {
			quoted = true
			for i := 1; i < len(source); i++ {
				switch source[i] {
				case '\\':
					i++
				case '"':
					next = source[1:i]
					line += strings.Count(next, "\n")
					source = source[i+1:]
					return
				}
			}
			t.Fatalf("%s:%d: unterminated string", path, line)
		}
		n := strings.IndexAny(source, "{} ,\t\r\n")
		if n < 0 {
			n = len(source)
		}
		if n == 0 {
			n = 1
		}
		next, source = source[:n], source[n:]
	}
	advance()
	var read func() resource
	read = func() resource {
		r := resource{
			line: line,
		}
		if next != "{" {
			r.name = next
			advance()
		}
		if next != "{" {
			t.Fatalf("%s:%d: expected resource body for %s, got %q", path, line, r.name, next)
		}
		advance()
		for next != "}" || quoted {
			switch {
			case quoted:
				r.values = append(r.values, next)
				advance()
			case next == ",":
				advance()
			case next == "":
				t.Fatalf("%s:%d: unclosed resource %s", path, line, r.name)
			default:
				r.children = append(r.children, read())
			}
		}
		advance()
		return r
	}
	root := read()
	if next != "" || quoted {
		t.Fatalf("%s:%d: trailing resource %q", path, line, next)
	}
	return root
}

func resourceCases(f *formatFixture, visit func(resource, map[string]string, int)) {
	root := readResource(f.t, f.path)
	for _, group := range root.child("TestData").children {
		headers := group.child("Headers").values
		for _, setting := range group.child("Settings").children {
			for _, row := range group.child("Cases").children {
				if len(headers) != len(row.values) {
					f.t.Fatalf("%s:%d: expected %d columns, got %v", f.path, row.line, len(headers), row.values)
				}
				fields := map[string]string{}
				for i, key := range headers {
					fields[key] = row.values[i]
				}
				visit(setting, fields, row.line)
			}
		}
	}
}

func integer(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(fmt.Sprintf("invalid fixture integer %q", s))
	}
	return n
}
