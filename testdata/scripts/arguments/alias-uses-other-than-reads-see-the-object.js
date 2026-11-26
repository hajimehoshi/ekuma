// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A use of a variable declared with the value of arguments other than a read of its length or elements sees the arguments object"
---*/

function pass() {
	var a = arguments;
	return [a.length, Object.prototype.toString.call(a)].join();
}
assert.sameValue(pass(1), "1,[object Arguments]");
