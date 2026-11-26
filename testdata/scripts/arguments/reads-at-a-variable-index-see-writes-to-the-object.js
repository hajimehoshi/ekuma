// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A read of arguments at an index in a variable, after a prototype getter received the arguments object, sees what the getter wrote to it"
---*/

Object.defineProperty(Object.prototype, "5", {
	get() {
		this[1] = "written";
		return 0;
	},
	configurable: true,
});

function f() {
	var i = 1;
	arguments[5];
	return arguments[i];
}
assert.sameValue(f("x", "y"), "written");

delete Object.prototype[5];
