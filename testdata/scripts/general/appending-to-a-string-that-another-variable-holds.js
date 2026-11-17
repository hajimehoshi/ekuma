// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "appending to a string that another variable holds"
flags: [noStrict]
---*/

var r = "";
for (var i = 0; i < 40; i++) r += "ab";
var s = r;
var t = r + "X";
var u = s + "Y";
var v = t + t;
r += "Z";
assert.sameValue(String([t.slice(78), u.slice(78), r.slice(78), s.slice(78), v.length, v.slice(79, 83),
	t === s + "X", u === r.slice(0, 80) + "Y"].join(" ")), "abX abY abZ ab 162 bXab true true");
