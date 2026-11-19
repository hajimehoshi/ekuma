// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "literal field initializers give their values, and undefined follows its binding"
flags: [noStrict]
---*/

class C {
	n = 1.5;
	s = "s";
	t = true;
	z = null;
	u = undefined;
	static k = "static";
	[Symbol.iterator] = 0;
}
var c = new C();
assert.sameValue([c.n, c.s, c.t, c.z, c.u, C.k, c[Symbol.iterator]].join(), "1.5,s,true,,,static,0");
assert.sameValue("u" in c, true);

assert.sameValue((function() {
	var undefined = 5;
	class D {
		u = undefined;
	}
	return new D().u;
})(), 5);

var E = class undefined {
	u = undefined;
};
assert.sameValue(new E().u, E);

assert.sameValue((function() {
	eval("var undefined = 6");
	class F {
		u = undefined;
	}
	return new F().u;
})(), 6);
