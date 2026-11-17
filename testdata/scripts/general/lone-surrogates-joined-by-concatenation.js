// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "lone surrogates joined by concatenation"
flags: [noStrict]
---*/

var h = "\uD83D", l = "\uDE00";
var s = "";
for (var i = 0; i < 40; i++) s += "a";
var t = s + h;
var u = t + l;
var w = `${s}${h}${l}`;
assert.sameValue(String([t.length, u.length, u.codePointAt(40).toString(16), u === s + "\u{1F600}", w === u,
	`${h}${l}` === "\u{1F600}"].join(" ")), "41 42 1f600 true true true");
