// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "loose equality of each kind of value"
---*/

// Pairs of values, each compared as values and in conditions with == and !=. The strings
// are literals, which are interned, or built at run time, short enough to be packed in the
// value or not. The last pairs need conversions.
function built(s) {
	return s.slice(0, 1) + s.slice(1);
}
var o = {}, a = [], sym = Symbol(), long = "a string that is too long to be packed";
var pairs = [
	[1, 1, true], [NaN, NaN, false], [0, -0, true],
	[o, o, true], [o, {}, false], [o, a, false], [a, a, true], [sym, sym, true],
	[undefined, undefined, true], [null, null, true], [null, undefined, true], [undefined, null, true],
	[null, 0, false], [undefined, false, false], [null, o, false], [a, undefined, false], ["", null, false],
	["ab", "ab", true], ["ab", "cd", false], ["ab", built("ab"), true], [built("ab"), built("cd"), false],
	[long, built(long), true], [long, "another string that is too long to pack", false],
	["1", 1, true], [true, 1, true], [{valueOf() { return 3; }}, 3, true], [1n, 1, true], [sym, "x", false],
];
function compare(x, y) {
	var r = [x == y, !(x != y)];
	if (x == y) r.push(true); else r.push(false);
	if (x != y) r.push(false); else r.push(true);
	return r;
}
var wrong = [];
for (var k = 0; k < 3; k++) {
	for (var p of pairs) {
		var r = compare(p[0], p[1]);
		if (r.some(function (v) { return v !== p[2]; })) wrong.push(String(p[0]) + " " + String(p[1]) + ": " + r.join());
	}
}
assert.sameValue(wrong.join("; "), "");
