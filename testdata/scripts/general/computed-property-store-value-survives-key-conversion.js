// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "computed property store value survives key conversion"
flags: [noStrict]
---*/

assert.sameValue((function () {
	let i = 1, stored;
	const o = { set x(v) { stored = v; } };
	const key = { toString() { i = 9; return "x"; } };
	o[key] = i;
	return stored;
})(), 1);
