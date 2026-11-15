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
// a name in any case finds the zone in the case of the database, and a link has the primary
// identifier of the zone it links to.
func TestLoadZone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the time zone database of Windows is not in files that can be matched without regard to case")
	}
	if _, _, _, ok := datetime.LoadZone("Asia/Kolkata"); !ok {
		t.Skip("no time zone database")
	}
	id, primary, _, ok := datetime.LoadZone("asia/KOLKATA")
	if !ok || id != "Asia/Kolkata" {
		t.Errorf("LoadZone(asia/KOLKATA): got %q, %v, want Asia/Kolkata, true", id, ok)
	}
	if _, linkPrimary, _, ok := datetime.LoadZone("Asia/Calcutta"); ok && linkPrimary != primary {
		t.Errorf("the primary identifiers of Asia/Calcutta and Asia/Kolkata differ")
	}
	if _, tokyo, _, ok := datetime.LoadZone("Asia/Tokyo"); ok && tokyo == primary {
		t.Errorf("the primary identifiers of Asia/Tokyo and Asia/Kolkata are the same")
	}
	if _, utc, _, ok := datetime.LoadZone("Etc/UTC"); ok && utc != "UTC" {
		t.Errorf("LoadZone(Etc/UTC): got the primary identifier %q, want UTC", utc)
	}
	if _, _, _, ok := datetime.LoadZone("Asia/Nowhere"); ok {
		t.Errorf("LoadZone(Asia/Nowhere): got true, want false")
	}
}
