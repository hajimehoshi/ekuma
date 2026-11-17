// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prefix update member base survives right operand"
flags: [noStrict]
---*/

assert.sameValue(String((function () {
	let i = 0;
	const o = {};
	(++i).x = (i = o, 1);
	return o.x;
})()), "undefined");
