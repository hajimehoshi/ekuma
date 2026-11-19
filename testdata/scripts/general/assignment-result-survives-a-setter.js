// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "assignment result survives a setter"
flags: [noStrict]
---*/

assert.sameValue(String((function () {
	let i = 1;
	const o = { set x(v) { i = 9; } };
	return [o.x = i, i].join();
})()), "1,9");
