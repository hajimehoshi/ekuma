// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package ekuma_test

import (
	_ "embed"
	"go/version"
	"regexp"
	"runtime"
	"testing"
	"unicode"

	"github.com/hajimehoshi/ekuma/internal/unicode/datafile"
)

//go:embed go.mod
var goMod string

func TestUnicodeVersion(t *testing.T) {
	match := regexp.MustCompile(`(?m)^[ \t]*go[ \t]+(\S+)`).FindStringSubmatch(goMod)
	if match == nil {
		t.Fatal("missing go directive in go.mod")
	}
	goRelease := version.Lang("go" + match[1])
	if version.Lang(runtime.Version()) != goRelease {
		t.Skipf("go.mod specifies %s, running %s", goRelease, runtime.Version())
	}
	if got, want := datafile.UnicodeVersion, unicode.Version; got != want {
		t.Errorf("datafile.UnicodeVersion = %q, want %q from %s", got, want, runtime.Version())
	}
}
