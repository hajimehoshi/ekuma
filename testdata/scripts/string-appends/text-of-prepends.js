// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Prepends give the strings that additions give, with their lengths and code units, and leave the strings that other values share as they were"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function grow(start, parts, n) {
	var r = start;
	for (var i = 0; i < n; i++) {
		r = "" + parts[i % parts.length] + r;
	}
	return r;
}
function slow(start, parts, n) {
	var r = start;
	for (var i = 0; i < n; i++) {
		r = [parts[i % parts.length], r].join("");
	}
	return r;
}

var cases = [
	[long, ["x", "abcdef"]],
	[long, [long, "y"]],
	[long, ["é", "x", "日本語"]],
	["é" + long, ["x", "y"]],
	[long, ["\ude00", "\ud83d"]],
	["\ude00" + long, ["x\ude00\ud83d"]],
	// Prepends that grow a builder, then a surrogate pair split over two prepends.
	[long, ["x", "x", "x", "x", "x", "\ude00", "\ud83d"]],
	// Prepends that grow a builder of ASCII, then one that is not ASCII.
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

// Lengths read between prepends that extend the strings in place.
function lengthsOnly(start, part) {
	var r = start, kept = [];
	for (var i = 0; i < 40; i++) {
		r = "" + part + r;
		kept.push(r.length);
	}
	kept.push(r);
	return kept;
}
// The last case forms a surrogate pair at each prepend.
cases = [[long, "x"], ["é" + long, "x"], [long, "日"], ["é" + long, "\u{1f600}"], ["\ude00" + long, "\ude00\ud83d"]];
for (var c = 0; c < cases.length; c++) {
	var got = lengthsOnly(cases[c][0], cases[c][1]);
	for (var i = 0; i < 40; i++) {
		assert.sameValue(got[i], cases[c][0].length + (i + 1) * cases[c][1].length, "length " + i + " of case " + c);
	}
	assert.sameValue(got[40], cases[c][1].repeat(40) + cases[c][0], "string of case " + c);
}

// Strings built by prepending, each prepended once to a variable that prepends made.
function prepended() {
	var r = long;
	for (var i = 0; i < 10; i++) {
		r = "x" + r;
	}
	for (var i = 0; i < 3; i++) {
		var p = long;
		for (var j = 0; j < 10; j++) {
			p = "p" + j + p;
		}
		r = p + r;
		r = "x" + r;
	}
	return r;
}
var p = long;
for (var j = 0; j < 10; j++) {
	p = ["p", j, p].join("");
}
var result = prepended();
assert.sameValue(result, ("x" + p).repeat(3) + "x".repeat(10) + long);

// A prepend whose result is a string that another variable holds.
function sharedPrefix() {
	var q = long;
	for (var i = 0; i < 10; i++) {
		q = "q" + q;
	}
	var r = "";
	for (var i = 0; i < 2; i++) {
		r = "<" + r;
	}
	r = "";
	r = q + r;
	for (var i = 0; i < 10; i++) {
		r = "r" + r;
	}
	return [q, r];
}
result = sharedPrefix();
assert.sameValue(result[0], "q".repeat(10) + long);
assert.sameValue(result[1], "r".repeat(10) + "q".repeat(10) + long);

// A prepend of the empty string to a string that another variable holds.
function sharedVariable() {
	var r = long, s;
	for (var i = 0; i < 10; i++) {
		r = "r" + r;
	}
	s = r;
	r = "" + r;
	for (var i = 0; i < 10; i++) {
		r = "x" + r;
	}
	return [s, r];
}
result = sharedVariable();
assert.sameValue(result[0], "r".repeat(10) + long);
assert.sameValue(result[1], "x".repeat(10) + "r".repeat(10) + long);
