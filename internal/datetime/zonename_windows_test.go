// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime_test

import (
	"os"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// TestLocalZoneName checks that the local time zone of Windows has an IANA time zone
// identifier.
func TestLocalZoneName(t *testing.T) {
	if _, ok := os.LookupEnv("TZ"); ok {
		t.Skip("TZ names the local time zone")
	}
	id, _, ok := datetime.ZoneOf(datetime.Local())
	if !ok || id == "" {
		t.Fatalf("ZoneOf(Local()): got %q, %v, want an identifier", id, ok)
	}
	if _, _, _, ok := datetime.LoadZone(id); !ok {
		t.Errorf("LoadZone(%q): got false, want true", id)
	}
}

func TestIANAZoneOfWindows(t *testing.T) {
	if got, ok := datetime.IANAZoneOfWindows("Tokyo Standard Time"); !ok || got != "Asia/Tokyo" {
		t.Errorf("IANAZoneOfWindows(Tokyo Standard Time): got %q, %v, want Asia/Tokyo, true", got, ok)
	}
	if got, ok := datetime.IANAZoneOfWindows("Nowhere Standard Time"); ok {
		t.Errorf("IANAZoneOfWindows(Nowhere Standard Time): got %q, true, want false", got)
	}
}
