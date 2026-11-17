// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "lone surrogates joined by prepending"
flags: [noStrict]
---*/

var h = "\uD83D", l = "\uDE00";
var s = "", e = h;
for (var i = 0; i < 40; i++) { s = "a" + s; e = "a" + e; }
var t = l + s;
var t2 = l + s;
var x = e + t2;
var w = `${h}${t}`;
var u = h + t;
assert.sameValue(String([t.length, u.length, u.codePointAt(0).toString(16), u === "\u{1F600}" + s, w === u,
	x.length, x === "a".repeat(40) + "\u{1F600}" + "a".repeat(40)].join(" ")), "41 42 1f600 true true 82 true");
