// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prefix update result survives destructuring"
flags: [noStrict]
---*/

assert.sameValue(String((function () {
	let i = 0, j;
	const result = ({ constructor: i, toFixed: j } = ++i);
	return [result, j === Number.prototype.toFixed].join();
})()), "1,true");
