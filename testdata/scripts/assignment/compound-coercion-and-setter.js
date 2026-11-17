// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "compound coercion and setter"
flags: [noStrict]
---*/

assert.sameValue((function() {
	let x = 3, seen = [];
	const o = {
		get p() { seen.push(x); return {valueOf() { x = 7; return 5; }}; },
		set p(v) { seen.push(x, v); x = 99; }
	};
	x = o.p += x;
	return [x, ...seen].join();
})(), "8,3,7,8");
