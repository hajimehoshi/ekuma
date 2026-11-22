// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

//go:build !js

package datetime_test

import (
	"archive/zip"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/hajimehoshi/ekuma/internal/datetime"
)

func TestLoadZoneFromZip(t *testing.T) {
	if runtime.GOOS == "wasip1" || runtime.GOOS == "ios" {
		t.Skip("requires a subprocess to isolate the time zone caches")
	}
	const name = "America/New_York"
	if os.Getenv("EKUMA_TEST_ZONEINFO_ZIP") == "1" {
		_, _, z, ok := datetime.LoadZone("america/new_york")
		if !ok {
			t.Fatalf("LoadZone(%q) failed", name)
		}
		if got := z.OffsetSeconds(0); got != 0 {
			t.Errorf("%s offset at the Unix epoch = %d seconds, want 0", name, got)
		}
		return
	}

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	entry, err := w.CreateHeader(&zip.FileHeader{
		Name:   name,
		Method: zip.Store,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A TZif v1 file with one UTC time type and no transitions.
	data := make([]byte, 50)
	copy(data, "TZif")
	data[39] = 1 // Number of local time types.
	data[43] = 4 // Number of bytes in the abbreviation table.
	data = append(data, "UTC\x00"...)
	if _, err := entry.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "zoneinfo.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	// Go records ZONEINFO on first use, and Ekuma caches each loaded zone.
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), "ZONEINFO="+path, "EKUMA_TEST_ZONEINFO_ZIP=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("test with ZONEINFO zip: %v\n%s", err, output)
	}
}

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
	// The link resolves to Europe/Oslo in the database, but Jan Mayen is in Svalbard and Jan
	// Mayen, whose only zone in zone.tab is Arctic/Longyearbyen.
	if _, primary, _, ok := datetime.LoadZone("Atlantic/Jan_Mayen"); !ok || primary != "Arctic/Longyearbyen" {
		t.Errorf("LoadZone(Atlantic/Jan_Mayen): got the primary identifier %q, %v, want Arctic/Longyearbyen, true", primary, ok)
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
