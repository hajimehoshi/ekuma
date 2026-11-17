// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a block-level function of eval code is hoisted over a catch parameter"
flags: [noStrict]
---*/

assert.sameValue(String((function () {
	try { throw 1; } catch (g) { eval("{ function g() {} }"); }
	return typeof g;
})()), "function");
