// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string units read at a warm site"
---*/

// Units read with constant and computed indices from strings of each kind: ASCII or not,
// short enough to be packed in the value or not, and built by appending or by prepending.
// An index past the end and one that is not an integer read String.prototype.
String.prototype[60] = "p";
String.prototype[1.5] = "f";
function units(s, i) {
	return [s[0], s[1], s[i], s[i + 1], s[i - 0.5], s[-0], s[60], s[-1]].join("|");
}
var appended = "", prepended = "";
for (var n = 0; n < 40; n++) {
	appended += "ab";
	prepended = "cd" + prepended;
}
var strings = [
	"the quick brown fox jumped over the lazy dog",
	"été and some more text after it",
	"abc",
	appended,
	prepended,
];
var r = [];
for (var k = 0; k < 3; k++) {
	r.push(strings.map(function (s) { return units(s, 2); }).join(" "));
}
var want = "t|h|e| |f|t|p| é|t|é| |f|é|p| a|b|c||f|a|p| a|b|a|b|f|a|a| c|d|c|d|f|c|c|";
assert.sameValue(r.join("\n"), [want, want, want].join("\n"));
