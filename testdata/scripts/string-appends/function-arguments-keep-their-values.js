// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "The arguments property of a running non-strict function keeps the values of the parameters that it read while appends to them go on"
flags: [noStrict]
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function grow(r) {
	var kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(peek());
	}
	return kept;
}
function peek() {
	return grow.arguments[0];
}

var kept = grow(long);
assert.sameValue(kept.length, 40);
for (var i = 0; i < 40; i++) {
	assert.sameValue(kept[i], long + "x".repeat(i + 1), "arguments " + i);
}

// The conversion of an appended value can read the parameter too.
function convert(r) {
	var kept = [];
	var x = { toString() { kept.push(convert.arguments[0]); return "x"; } };
	for (var i = 0; i < 40; i++) {
		r += "x";
		r += x;
	}
	return kept;
}
kept = convert(long);
assert.sameValue(kept.length, 40);
for (var i = 0; i < 40; i++) {
	assert.sameValue(kept[i], long + "x".repeat(2 * i + 1), "arguments during a conversion " + i);
}
