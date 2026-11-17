// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "auto-accessors"
flags: [noStrict]
---*/

var k = "x";
class A {
	accessor [k] = 1;
	static accessor #s = 2;
	static s() { return A.#s; }
}
class B extends A {}
var d = Object.getOwnPropertyDescriptor(A.prototype, "x"), a = new A(), b = new A(), out = [];
a.x = 3;
out.push(a.x, b.x, A.s(), Object.keys(a).length, d.enumerable, d.get.name, d.set.name, d.get.length, d.set.length);
try { d.get.call({}); } catch (e) { out.push(e.constructor.name); }
assert.sameValue(String(out.join()), "3,1,2,0,false,get x,set x,0,1,TypeError");
