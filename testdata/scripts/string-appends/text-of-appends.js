// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Appends of text that is not ASCII give the strings that additions give, with their lengths and code units"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function grow(start, parts, n) {
	var r = start;
	for (var i = 0; i < n; i++) {
		r += "" + parts[i % parts.length];
	}
	return r;
}
function slow(start, parts, n) {
	var r = start;
	for (var i = 0; i < n; i++) {
		r = [r, parts[i % parts.length]].join("");
	}
	return r;
}

var cases = [
	[long, ["é", "x", "日本語"]],
	[long, ["x", "\u{1f600}"]],
	["é" + long, ["x", "y"]],
	[long, ["\ud83d", "\ude00"]],
	[long, ["\ude00", "\ud83d"]],
	[long + "\ud83d", ["\ude00x\ud83d"]],
	// Appends that grow a builder, then a surrogate pair split over two appends.
	[long, ["x", "x", "x", "x", "x", "\ud83d", "\ude00"]],
	// Appends that grow a builder of ASCII, then one that is not ASCII.
	[long, ["x", "x", "x", "x", "x", "é"]],
];
for (var c = 0; c < cases.length; c++) {
	var got = grow(cases[c][0], cases[c][1], 50);
	var want = slow(cases[c][0], cases[c][1], 50);
	assert.sameValue(got, want, "case " + c);
	assert.sameValue(got.length, want.length, "length of case " + c);
	for (var i = 0; i < want.length; i += 7) {
		assert.sameValue(got.charCodeAt(i), want.charCodeAt(i), "case " + c + " at " + i);
	}
}

// Lengths read between appends.
function lengths() {
	var r = "é" + long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += i % 3 ? "x" : "日";
		kept.push(r.length, r.charCodeAt(r.length - 1));
	}
	return kept;
}
var kept = lengths();
for (var i = 0; i < 40; i++) {
	assert.sameValue(kept[2 * i], long.length + 2 + i, "length " + i);
	assert.sameValue(kept[2 * i + 1], i % 3 ? 0x78 : 0x65e5, "last unit " + i);
}

// The length of a string that is not ASCII, read before appends and after them.
function lengthAround(n) {
	var r = "é" + long;
	for (var i = 0; i < n; i++) {
		r += "x";
	}
	var before = r.length;
	for (var i = 0; i < n; i++) {
		r += "x";
	}
	return [before, r.length, r.charCodeAt(r.length - 1), r.charCodeAt(0)];
}
var around = lengthAround(20);
assert.sameValue(around[0], long.length + 21);
assert.sameValue(around[1], long.length + 41);
assert.sameValue(around[2], 0x78);
assert.sameValue(around[3], 0xe9);

// A string built by prepending, appended to a variable.
function prepended() {
	var p = "";
	for (var i = 0; i < 40; i++) {
		p = "p" + p;
	}
	var r = long;
	for (var i = 0; i < 5; i++) {
		r += "x";
		r += p;
	}
	return r;
}
assert.sameValue(prepended(), (long + ("x" + "p".repeat(40)).repeat(5)));
