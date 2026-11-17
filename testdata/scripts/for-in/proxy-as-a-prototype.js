// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "proxy as a prototype"
flags: [noStrict]
---*/

var log = [];
var p = new Proxy({a: 1, b: 2}, {
	ownKeys(t) { log.push("ownKeys"); return Reflect.ownKeys(t); },
	getOwnPropertyDescriptor(t, k) { log.push("gopd:" + k); return Reflect.getOwnPropertyDescriptor(t, k); },
	getPrototypeOf(t) { log.push("getPrototypeOf"); return null; },
});
var o = Object.setPrototypeOf({a: 0}, p);
var keys = [];
for (var k in o) keys.push(k);
assert.sameValue(String(keys.join() + " " + log.join()), "a,b ownKeys,gopd:b,getPrototypeOf");
