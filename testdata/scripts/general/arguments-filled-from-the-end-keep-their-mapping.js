// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments filled from the end keep their mapping"
flags: [noStrict]
---*/

function f(a, b) {
	for (var i = 99; i >= 2; i--) arguments[i] = i;
	a = "a"; arguments[1] = "b";
	return [arguments[0], b, arguments.length, arguments[50]].join();
}
assert.sameValue(String(f(1, 2)), "a,b,2,50");
