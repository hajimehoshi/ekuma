// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package datetime_test

import (
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
	for _, c := range []struct {
		name string
		want string
	}{
		{
			name: "Africa/CAIRO",
			want: "Africa/Cairo",
		},
		{
			// A zone whose primary identifier is UTC.
			name: "etc/UTC",
			want: "Etc/UTC",
		},
	} {
		if id, _, _, ok := datetime.LoadZone(c.name); !ok || id != c.want {
			t.Errorf("LoadZone(%q): got %q, %v, want %q, true", c.name, id, ok, c.want)
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
