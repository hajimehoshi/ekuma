// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "appending non-ASCII strings while reading the length and the end"
flags: [noStrict]
---*/

var s = "", n = 0, bad = [];
for (var i = 0; i < 40; i++) { s += "a"; n++; }
var parts = ["\u00e9", "x", "\u{1F600}"];
for (var i = 0; i < 60; i++) {
	var p = parts[i % 3];
	s += p;
	n += p.length;
	if (s.length !== n || s.charCodeAt(n - 1) !== p.charCodeAt(p.length - 1) || s.at(-p.length) !== p[0]) {
		bad.push(i);
	}
}
assert.sameValue(String(bad.length + " " + s.length + " " + (s.slice(-4) === "\u00e9x\u{1F600}") + " " + s.indexOf("\u00e9")), "0 120 true 40");
