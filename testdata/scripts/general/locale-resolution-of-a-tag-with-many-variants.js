// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "resolving a language tag with many variants takes time linear in its length"
flags: [onlyStrict]
---*/

var variants = [];
for (var i = 0; i < 400000; i++) {
	var v = "a";
	for (var k = 0, x = i; k < 5; k++, x = Math.floor(x / 26)) {
		v += String.fromCharCode(97 + x % 26);
	}
	variants.push(v + "b");
}
var tag = "en-" + variants.join("-");
assert.sameValue(new Intl.NumberFormat(tag).resolvedOptions().locale, "en", "NumberFormat");
assert.sameValue(new Intl.DateTimeFormat(tag).resolvedOptions().locale, "en", "DateTimeFormat");
assert.sameValue(new Intl.Collator(tag).resolvedOptions().locale, "en", "Collator");
assert.sameValue(Intl.NumberFormat.supportedLocalesOf(tag)[0].length, tag.length, "supportedLocalesOf");
