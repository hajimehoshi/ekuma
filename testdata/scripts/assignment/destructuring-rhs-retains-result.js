// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "destructuring RHS retains result"
flags: [noStrict]
---*/

assert.sameValue((function() {
	let x = [2, 3], seen;
	const o = {set p(v) { seen = x; x = [99]; }};
	const source = [5, 7];
	x = o.p = ([x] = source);
	return [x === source, seen].join();
})(), "true,5");
