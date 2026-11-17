// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "reading units and comparing a string while prepending to it"
flags: [noStrict]
---*/

var s = "", t = "", bad = [];
for (var i = 0; i < 200; i++) {
	var c = String.fromCharCode(97 + i % 26);
	s = c + s;
	t = c + t;
	if (s[0] !== c || s.charCodeAt(s.length - 1) !== 97 || s.at(-2) !== (i ? "b" : undefined) ||
		s !== t || s === "x" || s === "q".repeat(i)) bad.push(i);
}
s = "é" + s;
assert.sameValue(String(bad.length + " " + s.length + " " + s.slice(0, 4) + " " + s.charCodeAt(0).toString(16) + " " + (s === "é" + t)), "0 201 érqp e9 true");
