// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "logical not of each kind of value"
---*/

// The result of ! as a value for values of each kind, falsy ones first.
var values = [
	0, -0, NaN, undefined, null, false, "", 0n,
	1, -1, Infinity, true, "a", "a string that is too long to be packed", Symbol(), 1n, 2n ** 80n, {}, [], function () {},
];
function not(v) {
	return !v;
}
var r = [];
for (var k = 0; k < 3; k++) {
	r.push(values.map(not).join());
}
var want = "true,true,true,true,true,true,true,true,false,false,false,false,false,false,false,false,false,false,false,false";
assert.sameValue(r.join(" "), [want, want, want].join(" "));
