// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Reads of the length and the elements through a variable declared with the value of arguments see the arguments of the call"
---*/

function list() {
	var res = null;
	var a = arguments;
	for (var i = a.length - 1; i >= 0; i--) {
		res = [a[i], res];
	}
	return res;
}
assert.sameValue(list(), null);
assert.sameValue(JSON.stringify(list(1, 2)), "[1,[2,null]]");

// The reads see the arguments through a constant too, whose binding is uninitialized before its
// declaration.
function elements(x) {
	const a = arguments;
	return [a.length, a[0], a[1]].join();
}
assert.sameValue(elements(1, 2), "2,1,2");
