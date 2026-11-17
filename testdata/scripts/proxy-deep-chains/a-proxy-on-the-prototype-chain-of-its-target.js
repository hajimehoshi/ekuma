// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a proxy on the prototype chain of its target"
flags: [noStrict]
---*/

var caught;
try {
	var t = {};
	var p = new Proxy(t, {});
	Object.setPrototypeOf(t, p);
	"x" in p;
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
