// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

package icutest

func (s *durationSuite) xmlDurations(src intervalSource) {
	block := src.from("void TestXMLDuration()")
	for _, id := range []string{"PT10.00099S", "PT10S", "#10000"} {
		row := block.from("\"" + id + "\"")
		strs := listStrings(s.t, row.lines[0])
		fields := map[string]any{"seconds": 10}
		opts := map[string]any{"style": "long"}
		if id == "PT10.00099S" {
			fields = decimalDuration("10.00099", "seconds")
			opts["milliseconds"] = "numeric"
			opts["fractionalDigits"] = 0
		}
		s.check(row.line, "format", s.format, strs[2], strs[0], fields, opts)
	}
	s.excluded["XML formatter discards duration sign"] += 3
	s.excluded["legacy period list joining or spelled-out counts"] += 5
	s.excluded["TestFromNowTo elapsed time relative to an instant"] += len(src.from("void TestFromNowTo()").table("TestCase[] testCases ="))
}

func (s *durationSuite) periodFractions(src intervalSource, locale string) {
	// LanguageTestFmwk.testFractionalUnits iterates eight units, then
	// counts, with DECIMAL2 and past/future wording disabled.
	counts := src.from("=fractionalUnitCounts")
	targets := src.from("=fractionalUnitTargets")
	values := counts.lines[1 : targets.line-counts.line]
	end := targets.from("=multipleUnitTargets")
	if len(values) == 0 || end.line-targets.line-1 != 8*len(values) {
		s.t.Fatalf("%s:%d: invalid fractional-unit table", src.path, targets.line)
	}
	for j, unit := range []string{"seconds", "milliseconds"} {
		for i, amount := range values {
			opts := map[string]any{"style": "long", "fractionalDigits": 2}
			if unit == "seconds" {
				opts["milliseconds"] = "numeric"
			} else {
				opts["microseconds"] = "numeric"
			}
			// PeriodFormatter uses separate legacy locale data. Only compare
			// forms with the same unit, number and plural pattern in CLDR 49.
			switch {
			case locale == "es" && amount == "1.0", locale == "ar-EG" && unit == "seconds" && amount == "1.0":
				s.excluded["legacy singular pattern"]++
				continue
			case locale == "he-IL" && unit == "seconds" && amount == "0.025":
				s.excluded["legacy singular spelling"]++
				continue
			case locale == "zh-Hans-SG" && unit == "seconds",
				unit == "milliseconds" && (locale == "ar-EG" || locale == "he-IL" || locale == "ko"):
				s.excluded["legacy unit name"]++
				continue
			case locale == "th" && unit == "milliseconds":
				// The Thai period formatter expresses milliseconds as seconds.
				opts["milliseconds"] = "numeric"
				opts["fractionalDigits"] = 3
			}
			offset := 1 + (6+j)*len(values) + i
			s.check(targets.line+offset, "format", s.format, targets.lines[offset], locale, decimalDuration(amount, unit), opts)
		}
	}
	s.excluded["fixed decimals on years through minutes"] += 6 * len(values)
}
