// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "class with fields, accessors and inheritance"
flags: [noStrict]
---*/

class P { #secret = 7; base = 1; static count = 0;
	constructor(x) { this.x = x; P.count++; }
	get secret() { return this.#secret; }
	sum() { return this.x + this.base; } }
class Q extends P { extra = 10; constructor(x) { super(x); }
	sum() { return super.sum() + this.extra + this.secret; } }
assert.sameValue(String(new Q(5).sum() + ":" + P.count), "23:1");
