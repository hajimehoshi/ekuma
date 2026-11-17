// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for-in over a proxy"
flags: [noStrict]
---*/

var log = [];
var target = Object.create({ a: 1, b: 2, d: 5 });
Object.defineProperty(target, "a", { value: 0, enumerable: false });
target.b = 3;
target.c = 4;
var p = new Proxy(target, {
	ownKeys(t) { log.push("ownKeys"); return Reflect.ownKeys(t); },
	getOwnPropertyDescriptor(t, k) { log.push("gopd:" + k); return Reflect.getOwnPropertyDescriptor(t, k); },
	getPrototypeOf(t) { log.push("getPrototypeOf"); return Reflect.getPrototypeOf(t); },
});
var keys = [];
for (var k in p) keys.push(k);
assert.sameValue(String(keys.join() + " " + log.join()), "b,c,d ownKeys,gopd:a,gopd:b,gopd:c,getPrototypeOf");
