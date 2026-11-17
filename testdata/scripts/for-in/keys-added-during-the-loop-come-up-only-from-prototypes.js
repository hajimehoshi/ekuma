// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "keys added during the loop come up only from prototypes"
flags: [noStrict]
---*/

var a = Object.setPrototypeOf([10, 20], {3: 0});
var keys = [];
for (var k in a) {
	keys.push(k);
	if (k === "0") {
		a[2] = 30;
		a[3] = 40;
	}
}
assert.sameValue(String(keys.join()), "0,1,3");
