// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A getter on a prototype that a read through a variable declared with the value of arguments reaches receives the arguments object, which later reads of arguments use"
---*/

Object.defineProperty(Object.prototype, "5", {
	get() {
		return this;
	},
	configurable: true,
});

function expose(x) {
	var a = arguments;
	const args = a[5];
	args[0] = 9;
	args.length = 3;
	return [args === arguments[5], arguments[0], arguments.length, a[0], a.length].join();
}
assert.sameValue(expose(1), "true,9,3,9,3");

delete Object.prototype[5];
