// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "this before and after super"
flags: [noStrict]
---*/

class B extends Object {
	constructor() {
		const f = () => this;
		let r;
		try { f(); } catch (e) { r = e.name; }
		super();
		this.r = r + (f() === this);
	}
}
assert.sameValue(String(new B().r), "ReferenceErrortrue");
