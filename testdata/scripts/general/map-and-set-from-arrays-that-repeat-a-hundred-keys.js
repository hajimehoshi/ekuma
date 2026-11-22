// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Map and Set from arrays whose elements repeat a hundred distinct keys"
---*/

var src = [];
for (var i = 0; i < 1000; i++) {
	src.push(i % 100);
}
var s = new Set(src);
var m = new Map(src.map(function (k, i) { return [k, i]; }));
var keys = [];
for (var i = 0; i < 100; i++) {
	keys.push(i);
}
assert.sameValue([...s].join(), keys.join(), "the keys of the Set");
assert.sameValue([...m.keys()].join(), keys.join(), "the keys of the Map");
assert.sameValue(m.get(99), 999, "the last value of the last key");
