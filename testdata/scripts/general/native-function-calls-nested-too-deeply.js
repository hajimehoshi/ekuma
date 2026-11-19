// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "recursion through native function calls, which runs no interpreter loop, throws a RangeError"
flags: [noStrict]
---*/

var f = x => x;
for (let i = 0; i < 20000; i++) f = Function.prototype.call.bind(f, null);
var caught;
try {
	f(1);
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
