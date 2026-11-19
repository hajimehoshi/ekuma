// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "padStart and padEnd keep the UTF-16 code units of short strings and coerce their arguments in order"
flags: [noStrict]
---*/

function created(s) { return (s + "!").slice(0, -1); }
var cases = [
	["ab", "xy", 8, "xyxyxyab", "abxyxyxy"],
	["\ude00", "abc\ud83d", 5, "abc😀", "\ude00abc\ud83d"],
	["\ud83d", "\ude00abc", 5, "\ude00abc\ud83d", "😀abc"],
	["x", "😀abcdefgh", 2, "\ud83dx", "x\ud83d"],
	["x", "\ude00\ud83d", 4, "\ude00😀x", "x\ude00😀"],
	["é", "日", 4, "日日日é", "é日日日"],
	["", "\u0000", 1, "\u0000", "\u0000"]
];
for (var c of cases) {
	for (var dynamic of [false, true]) {
		var s = dynamic ? created(c[0]) : c[0];
		var f = dynamic ? created(c[1]) : c[1];
		assert.sameValue(s.padStart(c[2], f), c[3]);
		assert.sameValue(s.padEnd(c[2], f), c[4]);
	}
}
for (var method of ["padStart", "padEnd"]) {
	var log = "";
	var receiver = { toString() { log += "s"; return created("ab"); } };
	var target = { valueOf() { log += "n"; return 4; } };
	var fill = { toString() { log += "f"; return created("x"); } };
	String.prototype[method].call(receiver, target, fill);
	assert.sameValue(log, "snf");
	assert.sameValue(created("ab")[method](1, Symbol()), "ab");
	assert.sameValue(created("ab")[method](Infinity, ""), "ab");
}
