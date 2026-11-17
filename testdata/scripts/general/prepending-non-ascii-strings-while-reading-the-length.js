// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prepending non-ASCII strings while reading the length"
flags: [noStrict]
---*/

var s = "", n = 0, bad = [];
for (var i = 0; i < 40; i++) { s = "a" + s; n++; }
var parts = ["é", "x", "\u{1F600}"];
for (var i = 0; i < 60; i++) {
	var p = parts[i % 3];
	s = p + s;
	n += p.length;
	if (s.length !== n) bad.push(i);
}
assert.sameValue(String(bad.length + " " + s.length + " " + (s.slice(0, 4) === "\u{1F600}xé") + " " + s.indexOf("a") + " " + s.codePointAt(0).toString(16)), "0 120 true 80 1f600");
