// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "strict equality of each kind of value"
---*/

// Pairs of values, each compared as values and in conditions with === and !==. The strings
// are literals, which are interned, or built at run time, short enough to be packed in the
// value or not.
function built(s) {
	return s.slice(0, 1) + s.slice(1);
}
var o = {}, sym = Symbol(), long = "a string that is too long to be packed";
var pairs = [
	[1, 1, true], [1, 2, false], [NaN, NaN, false], [0, -0, true],
	[o, o, true], [o, {}, false], [sym, sym, true], [sym, Symbol(), false],
	[1n, 1n, true], [1n, 2n, false], [2n ** 80n, 2n ** 80n, true],
	[undefined, undefined, true], [null, null, true], [null, undefined, false],
	["ab", "ab", true], ["ab", "cd", false], ["ab", built("ab"), true], [built("ab"), built("cd"), false],
	[long, long, true], [long, built(long), true], [long, "another string that is too long to pack", false],
	[typeof 1, "number", true], [typeof "", "string", true], [typeof f, "function", true],
	[typeof undefined, "undefined", true], [typeof 1n, "string", false], [typeof {}, built("object"), true],
];
function f() {}
function compare(a, b) {
	var r = [a === b, !(a !== b)];
	if (a === b) r.push(true); else r.push(false);
	if (a !== b) r.push(false); else r.push(true);
	return r;
}
var wrong = [];
for (var k = 0; k < 3; k++) {
	for (var p of pairs) {
		var r = compare(p[0], p[1]);
		if (r.some(function (x) { return x !== p[2]; })) wrong.push(String(p[0]) + " " + String(p[1]) + ": " + r.join());
	}
}
assert.sameValue(wrong.join("; "), "");
