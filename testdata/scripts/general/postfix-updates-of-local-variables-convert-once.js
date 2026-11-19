// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "postfix updates of local variables convert once"
flags: [noStrict]
---*/

assert.sameValue((function () {
	let calls = 0;
	let x = { valueOf() { calls++; return 5; } };
	const oldX = x++;
	let y = 1n;
	const oldY = y--;
	return [oldX, x, calls, typeof oldY, oldY, y].join();
})(), "5,6,1,bigint,1,0");
