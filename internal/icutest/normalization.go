// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hajimehoshi/ekuma"
)

func runNormalizationTest(t *testing.T, path string) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// For each input c1 through c5, the fixture gives the expected column for each form.
	forms := []struct {
		name        string
		wantColumns [5]int
	}{
		{
			name:        "NFC",
			wantColumns: [5]int{2, 2, 2, 4, 4},
		},
		{
			name:        "NFD",
			wantColumns: [5]int{3, 3, 3, 5, 5},
		},
		{
			name:        "NFKC",
			wantColumns: [5]int{4, 4, 4, 4, 4},
		},
		{
			name:        "NFKD",
			wantColumns: [5]int{5, 5, 5, 5, 5},
		},
	}

	var vm ekuma.VM
	var rows, line int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line++
		data, _, _ := strings.Cut(scanner.Text(), "#")
		data = strings.TrimSpace(data)
		if data == "" || strings.HasPrefix(data, "@Part") {
			continue
		}
		var columns [5]ekuma.Value
		fields := strings.Split(strings.TrimSuffix(data, ";"), ";")
		if len(fields) != len(columns) {
			t.Fatalf("%s:%d: got %d columns, want %d", path, line, len(fields), len(columns))
		}
		for i, field := range fields {
			s, err := parseCodePoints(field)
			if err != nil {
				t.Fatalf("%s:%d: c%d: %v", path, line, i+1, err)
			}
			columns[i] = vm.ValueOf(s)
		}
		for _, form := range forms {
			for i, wantColumn := range form.wantColumns {
				want := columns[wantColumn-1]
				got, err := columns[i].Call(t.Context(), "normalize", form.name)
				if err != nil {
					t.Errorf("%s:%d: %s(c%d): got error %v, want c%d = %+q", path, line, form.name, i+1, err, wantColumn, want.String())
					continue
				}
				if !got.Equal(want) {
					t.Errorf("%s:%d: %s(c%d): got %+q, want c%d = %+q", path, line, form.name, i+1, got.String(), wantColumn, want.String())
				}
			}
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Fatalf("%s contains no normalization tests", path)
	}
	t.Logf("%d rows", rows)
}

func parseCodePoints(field string) (string, error) {
	var s strings.Builder
	for cp := range strings.FieldsSeq(field) {
		r, err := parseScalar(cp)
		if err != nil {
			return "", err
		}
		s.WriteRune(r)
	}
	if s.Len() == 0 {
		return "", fmt.Errorf("empty code point sequence")
	}
	return s.String(), nil
}

func parseScalar(field string) (rune, error) {
	n, err := strconv.ParseUint(field, 16, 32)
	if err != nil || !utf8.ValidRune(rune(n)) {
		return 0, fmt.Errorf("invalid Unicode scalar value %q", field)
	}
	return rune(n), nil
}
