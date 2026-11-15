// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package datetime_test

import (
	"runtime"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// TestLoadZone checks the identifiers of the time zones of the system's time zone database:
// a name in any case finds the zone in the case of the database, a link has the zone it
// resolves to as its primary identifier, and an identifier in zone.tab is primary.
func TestLoadZone(t *testing.T) {
	if _, _, _, ok := datetime.LoadZone("Asia/Kolkata"); !ok {
		t.Skip("no time zone database")
	}
	// The time zone database of Windows is not in files that can be matched without regard to
	// case.
	if runtime.GOOS != "windows" {
		if id, _, _, ok := datetime.LoadZone("asia/KOLKATA"); !ok || id != "Asia/Kolkata" {
			t.Errorf("LoadZone(asia/KOLKATA): got %q, %v, want Asia/Kolkata, true", id, ok)
		}
	}
	if id, primary, _, ok := datetime.LoadZone("asia/CALCUTTA"); !ok || id != "Asia/Calcutta" || primary != "Asia/Kolkata" {
		t.Errorf("LoadZone(asia/CALCUTTA): got %q, %q, %v, want Asia/Calcutta, Asia/Kolkata, true", id, primary, ok)
	}
	if _, oslo, _, ok := datetime.LoadZone("Europe/Oslo"); ok && oslo != "Europe/Oslo" {
		t.Errorf("LoadZone(Europe/Oslo): got the primary identifier %q, want Europe/Oslo", oslo)
	}
	if _, utc, _, ok := datetime.LoadZone("Etc/UTC"); ok && utc != "UTC" {
		t.Errorf("LoadZone(Etc/UTC): got the primary identifier %q, want UTC", utc)
	}
	if _, _, _, ok := datetime.LoadZone("Asia/Nowhere"); ok {
		t.Errorf("LoadZone(Asia/Nowhere): got true, want false")
	}
}
