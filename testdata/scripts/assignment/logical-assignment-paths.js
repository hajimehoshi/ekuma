// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "logical assignment paths"
flags: [noStrict]
---*/

assert.sameValue((function() {
	let x = 3, seen;
	const o = {get p() { return 5; }, set p(v) { seen = x; x = 99; }};
	x = o.p ||= 8;
	const skipped = x;
	x = o.p &&= x + 1;
	return [skipped, x, seen].join();
})(), "5,6,5");
