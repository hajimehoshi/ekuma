// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Appends to a variable convert their values as additions do, and keep the variable as it was when a conversion throws"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

// A conversion that throws leaves the variable with its value from before.
function throwing() {
	var r = long, caught = 0;
	var bad = { valueOf() { throw new Error("bad"); } };
	for (var i = 0; i < 40; i++) {
		r += "x";
		try {
			r += bad;
		} catch (e) {
			caught++;
			assert.sameValue(r, long + "x".repeat(i + 1), "after the throw " + i);
		}
	}
	return [r, caught];
}
var result = throwing();
assert.sameValue(result[0], long + "x".repeat(40));
assert.sameValue(result[1], 40);

// A conversion runs once per append, before the variable changes.
function converted() {
	var r = long, calls = [];
	var x = { toString() { calls.push("toString"); return "y"; }, valueOf() { return "z"; } };
	var y = { [Symbol.toPrimitive](hint) { calls.push(hint); return 7; } };
	for (var i = 0; i < 20; i++) {
		r += "x";
		r += x;
		r = r + y;
	}
	return [r, calls];
}
result = converted();
assert.sameValue(result[0], long + "xz7".repeat(20));
assert.sameValue(result[1].length, 20);
for (var i = 0; i < 20; i++) {
	assert.sameValue(result[1][i], "default", "hint " + i);
}

// A symbol cannot be converted.
function symbol() {
	var r = long;
	for (var i = 0; i < 40; i++) {
		r += "x";
	}
	try {
		r += Symbol();
	} catch (e) {
		assert.sameValue(e.constructor, TypeError);
		return r;
	}
	return "no error";
}
assert.sameValue(symbol(), long + "x".repeat(40));

// A variable that does not hold a string yet, or no longer does.
function notString(start) {
	var r = start, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		if (i === 20) {
			kept.push(r);
			r = 5;
		}
	}
	kept.push(r);
	return kept;
}
var undef = notString(undefined);
assert.sameValue(undef[0], "undefined" + "x".repeat(21));
assert.sameValue(undef[1], "5" + "x".repeat(19));
var num = notString(1.5);
assert.sameValue(num[0], "1.5" + "x".repeat(21));
assert.sameValue(num[1], "5" + "x".repeat(19));
var obj = notString({ toString() { return long; } });
assert.sameValue(obj[0], long + "x".repeat(21));

// A variable that a closure reads takes string constants without appends in place.
function captured(start) {
	var r = start;
	function get() { return r; }
	for (var i = 0; i < 40; i++) {
		r += "x";
		r = r + "y";
	}
	return get();
}
assert.sameValue(captured(undefined), "undefined" + "xy".repeat(40));
assert.sameValue(captured(1.5), "1.5" + "xy".repeat(40));
assert.sameValue(captured({ valueOf() { return 7; } }), "7" + "xy".repeat(40));
assert.sameValue(captured(long), long + "xy".repeat(40));

// Numbers appended after the variable became a string, and other compound assignments.
function numbers() {
	var r = "", kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		r += 1;
		r = r + 2;
	}
	kept.push(r);
	r += "3";
	r -= 0;
	kept.push(r);
	return kept;
}
result = numbers();
assert.sameValue(result[0], "x12".repeat(40));
assert.sameValue(result[1], NaN);
