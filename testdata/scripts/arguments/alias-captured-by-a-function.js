// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A function nested in another reads the arguments of the outer one through a variable declared with their value"
---*/

function outer() {
	var a = arguments;
	function inner() {
		return a[0];
	}
	return [a.length, inner()].join();
}
assert.sameValue(outer(1, 2), "2,1");
