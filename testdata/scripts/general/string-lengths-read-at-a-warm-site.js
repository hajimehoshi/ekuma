// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "string lengths read at a warm site"
---*/

// The lengths in UTF-16 code units of strings of each kind read at one site: ASCII or not,
// short enough to be packed in the value or not, and built by appending or by prepending.
function len(s) {
	return s.length;
}
var appended = "", prepended = "";
for (var i = 0; i < 40; i++) {
	appended += "ab";
	prepended = "cd" + prepended;
}
var strings = [
	"the quick brown fox jumped over the lazy dog",
	"the quick brown ᶠᵒˣ jumped over",
	"emoji \u{1F600} at the end of a longer string",
	"abc",
	"été",
	"",
	appended,
	prepended,
	appended + "é",
];
var r = [];
for (var k = 0; k < 3; k++) {
	r.push(strings.map(len).join());
}
assert.sameValue(r.join(" "), "44,31,38,3,3,0,80,80,81 44,31,38,3,3,0,80,80,81 44,31,38,3,3,0,80,80,81");
