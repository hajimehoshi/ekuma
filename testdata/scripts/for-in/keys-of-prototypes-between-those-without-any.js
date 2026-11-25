// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "keys of prototypes between those without any"
flags: [noStrict]
---*/

var hiding = (key, proto) => Object.create(proto, {[key]: {value: 0, configurable: true}});
var last = {y: 0, z: 0, added: 0};
var late = hiding("y", last);
var middle = Object.setPrototypeOf({m: 0, x: 0}, late);
var o = Object.setPrototypeOf({a: 0}, hiding("n", hiding("m", middle)));
var keys = [];
for (var k in o) {
	keys.push(k);
	if (k === "x") {
		late.w = 0;
		delete last.z;
	}
}
assert.sameValue(String(keys.join()), "a,x,w,added");
