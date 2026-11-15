// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package datetime_test

import (
	"testing"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

// TestLoadZoneIdentifier checks that a time zone of the JavaScript host has the given name in
// the case of the database as its identifier, not the name that the host resolves it to.
func TestLoadZoneIdentifier(t *testing.T) {
	if _, _, _, ok := datetime.LoadZone("Asia/Kolkata"); !ok {
		t.Skip("the host has no time zones")
	}
	for _, c := range []struct {
		name string
		want string
	}{
		{
			// A zone that some hosts resolve to its former name, Asia/Calcutta.
			name: "Asia/Kolkata",
			want: "Asia/Kolkata",
		},
		{
			name: "us/PACIFIC",
			want: "US/Pacific",
		},
	} {
		if id, _, _, ok := datetime.LoadZone(c.name); !ok || id != c.want {
			t.Errorf("LoadZone(%q): got %q, %v, want %q, true", c.name, id, ok, c.want)
		}
	}
}
