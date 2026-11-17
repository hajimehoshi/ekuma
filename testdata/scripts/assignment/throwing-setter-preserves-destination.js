// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "throwing setter preserves destination"
flags: [noStrict]
---*/

assert.sameValue((function() {
	let x = 3;
	const marker = {}, o = {set p(v) { throw marker; }};
	try { x = o.p = x + 1; } catch (e) {
		if (e !== marker) throw e;
	}
	return String(x);
})(), "3");
