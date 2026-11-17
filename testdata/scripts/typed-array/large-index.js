// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "large index"
flags: [noStrict]
---*/

// An index whose byte offset does not fit in 32 bits is out of bounds.
var a = new Float64Array([1.5]), r = [];
for (var i of [2 ** 29, 2 ** 31, 2 ** 32 - 2]) {
	a[i] = 2;
	r.push(a[i], a[0]);
}
assert.sameValue(String(r.join()), ",1.5,,1.5,,1.5");
