// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "setter observes old destination"
flags: [noStrict]
---*/

assert.sameValue((function() {
	let x = 3, seen;
	const o = {set p(v) { seen = [x, v]; x = 99; }};
	x = o.p = x * 2;
	return [x, ...seen].join();
})(), "6,3,6");
