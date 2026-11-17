// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "keys deleted before they come up do not hide the keys of prototypes"
flags: [noStrict]
---*/

var a = Object.setPrototypeOf([10, 20, 30], {0: 0, 1: 0, x: 0, y: 0});
a.x = 0;
a.y = 0;
var keys = [];
for (var k in a) {
	keys.push(k);
	if (k === "0") {
		delete a[0];
		delete a[1];
		delete a.y;
	}
}
assert.sameValue(String(keys.join()), "0,2,x,1,y");
