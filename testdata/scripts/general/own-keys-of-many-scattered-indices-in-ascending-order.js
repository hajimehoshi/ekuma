// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the own keys of an object with many scattered integer indices come in ascending order"
flags: [onlyStrict]
---*/

var o = {}, n = 100000;
for (var i = 0; i < n; i++) {
	o[(i * 2654435761) % 4294967291] = i;
}
o.name = 0;
var keys = Reflect.ownKeys(o);
assert.sameValue(keys.length, n + 1, "number of keys");
for (var i = 1; i < n; i++) {
	if (!(Number(keys[i - 1]) < Number(keys[i]))) {
		throw new Test262Error("keys " + (i - 1) + " and " + i + " out of order: " + keys[i - 1] + ", " + keys[i]);
	}
}
assert.sameValue(keys[n], "name", "string key after the indices");
