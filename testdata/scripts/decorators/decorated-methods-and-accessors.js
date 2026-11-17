// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "decorated methods and accessors"
flags: [noStrict]
---*/

function twice(v) { return function () { return v.call(this) * 2; }; }
class S {
	@twice m() { return 1; }
	@twice get g() { return 2; }
	@twice static #sm() { return 3; }
	@((v) => ({ get() { return v.get.call(this) * 2; }, set(x) { v.set.call(this, x + 1); } })) accessor a = 3;
	@((v) => ({ init: x => x + 100 })) @((v) => ({ init: x => x * 2 })) accessor #b = 1;
	@((v) => x => x + 1) @((v) => x => x * 3) f = 2;
	b() { return this.#b; }
	static sm() { return S.#sm(); }
}
var s = new S(), a = s.a;
s.a = 5;
assert.sameValue(String([s.m(), s.g, S.sm(), a, s.a, s.b(), s.f].join()), "2,4,6,6,12,102,7");
