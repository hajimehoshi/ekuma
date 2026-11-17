// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "block-level function arguments and parameter expressions"
flags: [noStrict]
---*/

// A parameter expression keeps the arguments object even with a function named
// arguments, and so stops the block-level function from being hoisted.
function f(a = 0) {
	function arguments() { return "top"; }
	{ function arguments() { return "block"; } }
	return arguments();
}
function g({a}) {
	function arguments() { return "top"; }
	{ function arguments() { return "block"; } }
	return arguments();
}
assert.sameValue(String(f() + " " + g({})), "top block");
