// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "function own keys after a store cached for an object with the same prototype"
flags: [noStrict]
---*/

function set(o) { o.x = 1; }
function f() {}
function* g() {}
var r = [];
for (var fn of [f, g]) {
	var proto = Object.getPrototypeOf(fn);
	set(Object.create(proto)); set(Object.create(proto)); set(fn);
	r.push(Object.getOwnPropertyNames(fn).join());
}
assert.sameValue(String(r.join("|")), "length,name,arguments,caller,prototype,x|length,name,prototype,x");
