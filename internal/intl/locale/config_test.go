// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package locale_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/intl/locale"
)

func TestLocaleFromConfig(t *testing.T) {
	config := "# LANG=ignored\nLANG=fr_FR.UTF-8\nLC_MESSAGES='ja_JP.UTF-8'\n"
	if got := locale.LocaleFromConfig(config); got != "ja_JP.UTF-8" {
		t.Errorf("locale config: got %q, want ja_JP.UTF-8", got)
	}
}
