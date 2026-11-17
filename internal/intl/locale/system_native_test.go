// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

// TODO: Remove the Android cgo restriction when purego supports Android without Cgo.
// Native Android locale detection currently requires Cgo through purego.

//go:build (android && cgo) || darwin || windows

package locale_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestHostLocaleSystem(t *testing.T) {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(key, "")
	}
	got := locale.HostLocale()
	if _, ok := locale.FromHost(got); !ok {
		t.Errorf("system locale %q is not a valid host locale", got)
	}
	t.Logf("system locale: %q", got)
}
