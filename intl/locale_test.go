// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package intl_test

import (
	"context"
	"testing"

	"github.com/hajimehoshi/ekuma"
)

func evalLocale(t *testing.T, r *ekuma.Realm, source string) string {
	t.Helper()
	s, err := r.ParseScript("locale.js", source)
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.Evaluate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return v.String()
}

func TestSetLocale(t *testing.T) {
	var vm ekuma.VM
	if err := vm.SetLocale("de-de"); err != nil {
		t.Fatal(err)
	}
	r := vm.InitialRealm()
	if got := evalLocale(t, r, `new Intl.NumberFormat().resolvedOptions().locale`); got != "de-DE" {
		t.Errorf("canonical locale: got %q, want de-DE", got)
	}
	if got := evalLocale(t, r, `(1234.5).toLocaleString()`); got != "1.234,5" {
		t.Errorf("default formatting: got %q, want 1.234,5", got)
	}
	if got := evalLocale(t, r, `new Intl.NumberFormat("en-US").format(1234.5)`); got != "1,234.5" {
		t.Errorf("explicit locale: got %q, want 1,234.5", got)
	}
	if got := evalLocale(t, r, `new Intl.NumberFormat("tlh").resolvedOptions().locale`); got != "de-DE" {
		t.Errorf("unmatched request: got %q, want de-DE", got)
	}
	if err := vm.SetLocale("not_a_locale"); err == nil {
		t.Error("SetLocale accepted a malformed locale")
	}
	if got := evalLocale(t, r, `new Intl.NumberFormat().resolvedOptions().locale`); got != "de-DE" {
		t.Errorf("locale after failed setter: got %q, want de-DE", got)
	}
}

func TestLocaleMatching(t *testing.T) {
	var vm ekuma.VM
	if err := vm.SetLocale("ja"); err != nil {
		t.Fatal(err)
	}
	r := vm.InitialRealm()
	for _, tc := range []struct {
		matcher string
		want    string
	}{
		{
			matcher: "undefined",
			want:    "fr-HT-u-nu-arab|ht-u-nu-arab",
		},
		{
			matcher: "'best fit'",
			want:    "fr-HT-u-nu-arab|ht-u-nu-arab",
		},
		{
			matcher: "'lookup'",
			want:    "ja|",
		},
	} {
		got := evalLocale(t, r, `{
			const options = {localeMatcher: `+tc.matcher+`};
			const requests = ["ht-Cyrl-u-nu-thai", "HT-u-nu-arab"];
			const locale = new Intl.NumberFormat(requests, options).resolvedOptions().locale;
			locale + "|" + Intl.NumberFormat.supportedLocalesOf(requests, options).join();
		}`)
		if got != tc.want {
			t.Errorf("localeMatcher %s: got %q, want %q", tc.matcher, got, tc.want)
		}
	}
}

func TestBestFitPrefersRequestedLanguage(t *testing.T) {
	var vm ekuma.VM
	got := evalLocale(t, vm.InitialRealm(), `
		new Intl.NumberFormat(["co-u-nu-arab", "de-Latn-AT-u-nu-thai"]).resolvedOptions().locale
	`)
	if got != "de-AT-u-nu-thai" {
		t.Errorf("resolved locale: got %q, want de-AT-u-nu-thai", got)
	}
}

func TestSetLocaleChanges(t *testing.T) {
	var vm, other ekuma.VM
	if err := vm.SetLocale("en-US"); err != nil {
		t.Fatal(err)
	}
	if err := other.SetLocale("fr"); err != nil {
		t.Fatal(err)
	}
	realms := []*ekuma.Realm{vm.InitialRealm(), vm.NewRealm()}
	for _, r := range realms {
		evalLocale(t, r, `globalThis.oldFormat = new Intl.NumberFormat(); "ä".localeCompare("z")`)
	}
	if err := vm.SetLocale("sv"); err != nil {
		t.Fatal(err)
	}
	realms = append(realms, vm.NewRealm())
	for i, r := range realms {
		if got := evalLocale(t, r, `String("ä".localeCompare("z") > 0)`); got != "true" {
			t.Errorf("realm %d: cached collation: got %q, want true", i, got)
		}
		if got := evalLocale(t, r, `new Intl.NumberFormat().resolvedOptions().locale`); got != "sv" {
			t.Errorf("realm %d: new formatter: got %q, want sv", i, got)
		}
		if i < 2 {
			if got := evalLocale(t, r, `oldFormat.resolvedOptions().locale`); got != "en-US" {
				t.Errorf("realm %d: existing formatter: got %q, want en-US", i, got)
			}
		}
	}
	if got := evalLocale(t, other.InitialRealm(), `new Intl.NumberFormat().resolvedOptions().locale`); got != "fr" {
		t.Errorf("other VM: got %q, want fr", got)
	}
	if err := vm.SetLocale("tr"); err != nil {
		t.Fatal(err)
	}
	if got := evalLocale(t, realms[0], `"i".toLocaleUpperCase()`); got != "İ" {
		t.Errorf("locale-sensitive case: got %q, want İ", got)
	}
}

func TestSetLocaleHostFallback(t *testing.T) {
	var vm, host ekuma.VM
	want := evalLocale(t, host.InitialRealm(), `new Intl.NumberFormat().resolvedOptions().locale`)
	override := "ja"
	if want == override {
		override = "de"
	}
	for _, tag := range []string{"", "tlh"} {
		if err := vm.SetLocale(override); err != nil {
			t.Fatal(err)
		}
		if err := vm.SetLocale(tag); err != nil {
			t.Fatal(err)
		}
		if got := evalLocale(t, vm.InitialRealm(), `new Intl.NumberFormat().resolvedOptions().locale`); got != want {
			t.Errorf("SetLocale(%q): got %q, want host locale %q", tag, got, want)
		}
	}
}
