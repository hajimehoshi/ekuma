// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "destructuring assignment source survives assignments"
flags: [noStrict]
---*/

assert.sameValue(String((function () {
	let i = 0, j;
	const result = ({ constructor: i, toFixed: j } = i);
	return [result, j === Number.prototype.toFixed].join();
})()), "0,true", "the pattern assigns the source");

assert.sameValue(String((function () {
	let i = { get a() { i = null; return 1; }, b: 2 }, a, b;
	const result = ({ a, b } = i);
	return [typeof result, a, b].join();
})()), "object,1,2", "a getter assigns the source");
