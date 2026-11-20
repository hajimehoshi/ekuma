// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "an array literal of constants gives a new array of ordinary elements at each evaluation, which changes to an earlier one do not reach, and a shadowed undefined in it is the variable"
---*/

function make() {
	return [1, "two", 3n, true, false, null, undefined, void 0, -0, -4.5];
}

var a = make();
var b = make();
assert.sameValue(a === b, false);
a[0] = 100;
a[6] = "six";
a.push(11);
b.length = 2;

var c = make();
assert.sameValue(Array.isArray(c), true);
assert.sameValue(Object.getPrototypeOf(c), Array.prototype);
assert.sameValue(c.length, 10);
assert.sameValue(c[0], 1);
assert.sameValue(c[1], "two");
assert.sameValue(c[2], 3n);
assert.sameValue(c[3], true);
assert.sameValue(c[4], false);
assert.sameValue(c[5], null);
assert.sameValue(c[6], undefined);
assert.sameValue(6 in c, true);
assert.sameValue(c[7], undefined);
assert.sameValue(7 in c, true);
assert.sameValue(Object.is(c[8], -0), true);
assert.sameValue(c[9], -4.5);
var d = Object.getOwnPropertyDescriptor(c, 0);
assert.sameValue(d.writable && d.enumerable && d.configurable, true);
c.push(12);
assert.sameValue(c.length, 11);

assert.sameValue((function () {
	var undefined = 1;
	return [undefined, 2].join();
})(), "1,2");

assert.sameValue(Function("o", "with (o) { return [undefined, 2].join(); }")({undefined: 3}), "3,2");
