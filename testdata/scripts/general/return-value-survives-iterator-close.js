// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "return value survives closing an iterator"
flags: [noStrict]
---*/

assert.sameValue((function () {
	let x = 1;
	const it = {
		[Symbol.iterator]() { return this; },
		next() { return { done: false }; },
		return() { x = 2; return {}; }
	};
	for (const v of it) {
		return x;
	}
})(), 1);
