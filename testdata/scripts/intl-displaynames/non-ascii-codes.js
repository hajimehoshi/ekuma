// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "region and script codes require ASCII before case conversion"
---*/

// The Kelvin sign lowercases to ASCII "k".
for (const [type, code] of [["region", "UK"], ["script", "Kana"]]) {
	const names = new Intl.DisplayNames("en", {type});
	let caught;
	try {
		names.of(code);
	} catch (error) {
		caught = error;
	}
	assert.sameValue(caught instanceof RangeError, true);
}
