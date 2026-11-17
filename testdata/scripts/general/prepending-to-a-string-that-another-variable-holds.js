// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prepending to a string that another variable holds"
flags: [noStrict]
---*/

var r = "";
for (var i = 0; i < 40; i++) r = "ab" + r;
var s = r;
var t = "X" + r;
var u = "Y" + s;
var g = u + "G";
var v = t + t;
var w = r + "Z";
var e = s + t;
r = "Q" + r;
var q = "";
for (var i = 0; i < 40; i++) q = "cd" + q;
var qq = q + q;
assert.sameValue(String([t.slice(0, 3), u.slice(0, 3), g.slice(-2), r.slice(0, 3), s.slice(0, 2), w.slice(-3), v.length,
	v.slice(79, 84), t === "X" + s, u === "Y" + "ab".repeat(40),
	e === "ab".repeat(40) + "X" + "ab".repeat(40), qq === "cd".repeat(80)].join(" ")), "Xab Yab bG Qab ab abZ 162 abXab true true true true");
