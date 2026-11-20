// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the arguments of a method call are evaluated after the method is read, so a getter of the method can change a variable passed as an argument"
---*/

assert.sameValue((function () {
	var x = 1;
	var o = {get m() { x = 2; return function (v) { return v; }; }};
	return o.m(x);
})(), 2);
