// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prefix update operands convert once"
flags: [noStrict]
---*/

assert.sameValue(String((function () {
	let calls = 0;
	let i = { valueOf() { calls++; return "1"; } };
	let j = 3n;
	const a = [10, 20, 30];
	return [a[++i], a[--j], i, j, calls].join();
})()), "30,30,2,2,1");
