// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a block variable assigned by one closure is seen by another, after an earlier block returned a closure"
---*/

function f(c) {
	if (c) {
		let x = 1;
		x = 2;
		return () => x;
	}
	{
		let y = 0;
		const get = () => y;
		const set = () => { y = 3; };
		return [get, set];
	}
}
var r = f(false);
r[1]();
assert.sameValue(r[0](), 3);
assert.sameValue(f(true)(), 2);
