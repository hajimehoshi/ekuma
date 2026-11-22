// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "unit words that express the number preserve signs and range endpoints"
---*/

const options = {style: "unit", unit: "year", unitDisplay: "long"};
const format = new Intl.NumberFormat("ar", options);
assert.sameValue(format.format(-2), "\u200e-سنتان");
assert.sameValue(format.format(2), "سنتان");

const expected = [
	["literal", "\u200e", "startRange"],
	["minusSign", "-", "startRange"],
	["integer", "2", "startRange"],
	["literal", " – ", "shared"],
	["integer", "2", "endRange"],
	["literal", " ", "shared"],
	["unit", "سنة", "shared"],
];
assert.sameValue(format.formatRange(-2, 2), expected.map(p => p[1]).join(""));
const parts = format.formatRangeToParts(-2, 2);
assert.sameValue(parts.length, expected.length);
for (const [i, part] of parts.entries()) {
	const [type, value, source] = expected[i];
	assert.sameValue(part.type, type);
	assert.sameValue(part.value, value);
	assert.sameValue(part.source, source);
}

const unsigned = new Intl.NumberFormat("ar", {...options, signDisplay: "never"});
const approximate = unsigned.formatRangeToParts(-2, 2);
assert.sameValue(approximate.length, 2);
assert.sameValue(approximate[0].type, "approximatelySign");
assert.sameValue(approximate[1].value, "سنتان");
assert.sameValue(approximate.every(p => p.source === "shared"), true);

// The Danish range from zero to one selects a unit pattern with no number.
const danish = new Intl.NumberFormat("da", {...options, unit: "mile-per-gallon"});
assert.sameValue(danish.formatRange(0, 1), "0-1 mil pr. gallon");
