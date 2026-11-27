// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a[++i] and a[i++] update the counter before the read, which code that the read runs sees"
---*/

// The counter is captured, so the getter can read it. The elements before and after the
// getter are read without it.
function reads(a) {
	var i = -1, j = 0;
	Object.defineProperty(a, 2, { get() { return "i=" + i + " j=" + j; } });
	return [a[++i], a[++i], a[++i], a[++i], a[j++], a[j++], a[j++], a[j++]];
}
for (var n = 0; n < 3; n++) {
	assert.sameValue(JSON.stringify(reads([0, 1, 2, 3])), '[0,1,"i=2 j=0",3,0,1,"i=3 j=3",3]');
}
