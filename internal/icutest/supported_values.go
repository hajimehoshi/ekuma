// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

func checkNumberingSystemValues(f *formatFixture) {
	systems := readResource(f.t, f.path).child("numberingSystems")
	var names []string
	for _, system := range systems.children {
		if resourceValue(f, system.child("algorithmic:int")) == "1" {
			f.excluded["algorithmic numbering system without a simple digit mapping"]++
			continue
		}
		if resourceValue(f, system.child("radix:int")) != "10" {
			f.excluded["nondecimal numbering system"]++
			continue
		}
		names = append(names, system.name)
	}
	checkSupportedValues(f, systems.line, "numberingSystem", names)
}

func checkCollationResources(f *formatFixture, mapping resource) {
	files, err := filepath.Glob(filepath.Join(filepath.Dir(f.path), "*.txt"))
	if err != nil {
		f.t.Fatal(err)
	}
	all := map[string]bool{}
	local := map[string][]string{}
	for _, path := range files {
		data := readResource(f.t, path)
		for _, entry := range data.child("collations").children {
			switch {
			case entry.name == "default":
				// This entry names a default tailoring.
				continue
			case entry.name == "standard", entry.name == "search":
				f.excluded["reserved collation type"]++
			case strings.HasPrefix(entry.name, "private-"):
				f.excluded["private ICU tailoring"]++
			default:
				name := unicodeType(f, mapping, entry.name)
				local[data.name] = append(local[data.name], name)
				all[name] = true
			}
		}
	}
	root := readResource(f.t, f.path).child("collations")
	checkSupportedValues(f, root.line, "collation", slices.Collect(maps.Keys(all)))
	fn := f.function(`tag => new Intl.Locale(tag).getCollations().join(" ")`)
	// The root tailorings are inherited even by locales with their own default.
	for _, tag := range []string{"und", "de-DE-1996", "zh-Hant-TW", "ko-KR", "es-MX"} {
		language, _, _ := strings.Cut(tag, "-")
		names := append(slices.Clone(local["root"]), local[language]...)
		slices.Sort(names)
		f.check(root.line, "getCollations", fn, strings.Join(slices.Compact(names), " "), tag)
	}
	phonebook := unicodeType(f, mapping, "phonebook")
	f.check(root.line, "getCollations", fn, phonebook, "zh-u-co-"+phonebook)
}

func checkUnitValues(f *formatFixture) {
	// ECMA-402's sanctioned single units select the applicable ICU display names.
	const sanctioned = "acre bit byte celsius centimeter day degree fahrenheit fluid-ounce foot gallon " +
		"gigabit gigabyte gram hectare hour inch kilobit kilobyte kilogram kilometer liter megabit " +
		"megabyte meter microsecond mile mile-scandinavian milliliter millimeter millisecond minute " +
		"month nanosecond ounce percent petabyte pound second stone terabit terabyte week yard year"
	units := readResource(f.t, f.path).child("units")
	single := strings.Fields(sanctioned)
	var names []string
	for _, category := range units.children {
		for _, unit := range category.children {
			if !slices.Contains(single, unit.name) {
				f.excluded["ICU unit or pattern outside the sanctioned single-unit table"]++
				continue
			}
			names = append(names, unit.name)
		}
	}
	checkSupportedValues(f, units.line, "unit", names)
}

func checkZoneValues(f *formatFixture) {
	data := readResource(f.t, f.path)
	types := data.child("typeMap").child("timezone")
	iana := data.child("ianaMap").child("timezone")
	path := filepath.Join(filepath.Dir(f.path), "zoneinfo64.txt")
	rules := readResource(f.t, path)
	// ICU indexes the parallel Names and Regions arrays by zone number.
	ids, regions := rules.child("Names").values, rules.child("Regions:array").values
	if len(ids) == 0 || len(ids) != len(regions) {
		f.t.Fatalf("%s: got %d names and %d regions", path, len(ids), len(regions))
	}
	regionOf := map[string]string{}
	for i, id := range ids {
		regionOf[id] = regions[i]
	}
	global := map[string]bool{}
	regional := map[string][]string{}
	for _, entry := range types.children {
		id := strings.ReplaceAll(entry.name, ":", "/")
		if replacement := iana.child(entry.name); replacement.line != 0 {
			id = resourceValue(f, replacement)
		}
		switch id {
		case "Etc/Unknown":
			f.excluded["ICU unknown-zone sentinel"]++
			continue
		case "Etc/UTC", "Etc/GMT":
			id = "UTC"
		}
		region, ok := regionOf[id]
		if !ok {
			f.t.Fatalf("%s:%d: no region for %s in %s", f.path, entry.line, id, path)
		}
		global[id] = true
		if region != "001" {
			regional[region] = append(regional[region], id)
		}
	}
	checkSupportedValues(f, types.line, "timeZone", slices.Collect(maps.Keys(global)))
	fn := f.function(`tag => {
		const zones = new Intl.Locale(tag).getTimeZones();
		return zones === undefined ? "undefined" : zones.join(" ");
	}`)
	for _, region := range slices.Sorted(maps.Keys(regional)) {
		names := regional[region]
		slices.Sort(names)
		f.check(types.line, "getTimeZones", fn, strings.Join(names, " "), "und-"+region)
	}
	f.check(types.line, "getTimeZones", fn, strings.Join(regional["IN"], " "), "en-IN-u-rg-uszzzz")
	f.check(types.line, "getTimeZones", fn, "", "en-001")
	f.check(types.line, "getTimeZones", fn, "undefined", "en-u-rg-uszzzz")
}
