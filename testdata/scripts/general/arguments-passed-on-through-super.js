// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments passed on through super"
flags: [noStrict]
---*/

class A { constructor(x) { this.x = x; } m(y) { return this.x + y; } }
class B extends A { constructor() { super(...arguments); } m() { return super.m.apply(this, arguments); }
	n() { return super.apply(this, arguments); } }
A.prototype.apply = function(t, a) { return a.length; };
assert.sameValue(String(new B(5).x + "," + new B(5).m(1) + "," + new B(5).n(1, 2)), "5,6,2");
