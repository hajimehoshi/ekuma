// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "trailingZeroDisplay strips fraction digits when the rounded result is an integer"
---*/

const auto = new Intl.NumberFormat("en", {minimumFractionDigits: 2});
assert.sameValue(auto.resolvedOptions().trailingZeroDisplay, "auto");
assert.sameValue(auto.format(1), "1.00");

const options = {minimumFractionDigits: 2, trailingZeroDisplay: "stripIfInteger"};
const strip = new Intl.NumberFormat("en", options);
assert.sameValue(strip.resolvedOptions().trailingZeroDisplay, "stripIfInteger");
assert.sameValue(strip.format(1), "1");
assert.sameValue(strip.format(0), "0");
assert.sameValue(strip.format(1.5), "1.50");
assert.sameValue(strip.formatRange(1, 2), "1–2");

const integer = strip.formatToParts(2);
assert.sameValue(integer.length, 1);
assert.sameValue(integer[0].type, "integer");
assert.sameValue(integer[0].value, "2");

const fractional = strip.formatToParts(1.5);
assert.sameValue(fractional.length, 3);
assert.sameValue(fractional[0].type, "integer");
assert.sameValue(fractional[0].value, "1");
assert.sameValue(fractional[1].type, "decimal");
assert.sameValue(fractional[1].value, ".");
assert.sameValue(fractional[2].type, "fraction");
assert.sameValue(fractional[2].value, "50");

const rounded = new Intl.NumberFormat("en", {maximumFractionDigits: 0, trailingZeroDisplay: "stripIfInteger"});
assert.sameValue(rounded.format(1.5), "2");

const significant = new Intl.NumberFormat("en", {
	minimumSignificantDigits: 3,
	maximumSignificantDigits: 3,
	trailingZeroDisplay: "stripIfInteger",
});
assert.sameValue(significant.format(1.004), "1");

const plural = new Intl.PluralRules("en", options);
assert.sameValue(plural.resolvedOptions().trailingZeroDisplay, "stripIfInteger");
assert.sameValue(plural.select(1), "one");

const slAuto = new Intl.PluralRules("sl", {minimumFractionDigits: 2});
const slStrip = new Intl.PluralRules("sl", options);
assert.sameValue(slAuto.selectRange(1, 2), "few");
assert.sameValue(slStrip.selectRange(1, 2), "two");
