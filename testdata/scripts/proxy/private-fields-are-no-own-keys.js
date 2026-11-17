// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "private fields are no own keys"
flags: [noStrict]
---*/

class A {
	#x = 1;
	static setX(o) { o.#x = 2; return o.#x; }
}
var a = new A();
Object.freeze(a);
var keys = [];
Object.getOwnPropertySymbols(new Proxy(a, { ownKeys(t) { var k = Reflect.ownKeys(t); keys = k; return k; } }));
assert.sameValue(String(A.setX(a) + " " + keys.length), "2 0");
