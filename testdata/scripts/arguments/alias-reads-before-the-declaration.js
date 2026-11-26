// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A read through a variable before its declaration with the value of arguments is evaluated does not see the arguments"
---*/

function read() {
	var before;
	try {
		before = a.length;
	} catch (e) {
		before = e.constructor;
	}
	let a = arguments;
	return [before, a.length];
}
var r = read(1);
assert.sameValue(r[0], ReferenceError);
assert.sameValue(r[1], 1);
