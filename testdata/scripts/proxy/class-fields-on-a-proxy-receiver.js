// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "class fields on a proxy receiver"
flags: [noStrict]
---*/

var log = [];
class Base {
	constructor() {
		return new Proxy({}, { defineProperty(t, k, d) { log.push(k); return Reflect.defineProperty(t, k, d); } });
	}
}
class C extends Base {
	x = 1;
	#y = 2;
	static y(o) { return o.#y; }
	static hasY(o) { return #y in o; }
}
var c = new C();
assert.sameValue(String(log.join() + " " + C.y(c) + " " + C.hasY(c) + " " + Reflect.ownKeys(c).join()), "x 2 true x");
