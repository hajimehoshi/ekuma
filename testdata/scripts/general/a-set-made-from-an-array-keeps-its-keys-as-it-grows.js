// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a Set made from an array keeps its keys as more are added"
---*/

var src = [];
for (var i = 0; i < 40; i++) {
	src.push(i);
}
var s = new Set(src);
for (var i = 40; i < 150; i++) {
	s.add(i);
}
var keys = [];
for (var i = 0; i < 150; i++) {
	keys.push(i);
	assert.sameValue(s.has(i), true, "has " + i);
}
assert.sameValue([...s].join(), keys.join(), "the keys in order");
