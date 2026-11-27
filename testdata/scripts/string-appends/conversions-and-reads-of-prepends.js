// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Prepends convert their operands in order as additions do, keep the variable as it was when a conversion throws, and leave the values read before or between them as they were"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

// A conversion that throws leaves the variable with its value from before, and the prepends
// after it go on.
function throwing() {
	var r = long, lengths = [];
	var bad = { valueOf() { throw new Error("bad"); } };
	for (var i = 0; i < 40; i++) {
		r = "x" + r;
		try {
			r = bad + r;
		} catch (e) {
			lengths.push(r.length);
		}
	}
	return [r, lengths];
}
var result = throwing();
assert.sameValue(result[0], "x".repeat(40) + long);
assert.sameValue(result[1].length, 40);
for (var i = 0; i < 40; i++) {
	assert.sameValue(result[1][i], long.length + i + 1, "length after the throw " + i);
}

// The value is converted before the variable, and the variable need not hold a string.
function order() {
	var calls = [];
	function prim(name, v) {
		return { [Symbol.toPrimitive](hint) { calls.push(name + ":" + hint); return v; } };
	}
	var r = "", kept = [];
	for (var i = 0; i < 3; i++) {
		r = "<" + r;
		kept.push(r);
		r = prim("r", "R");
		r = prim("x", "X") + r;
		kept.push(r);
		r = prim("r", 7);
		r = "<" + r;
		kept.push(r);
	}
	return [kept, calls];
}
result = order();
assert.sameValue(result[0].join(), "<,XR,<7,<<7,XR,<7,<<7,XR,<7");
assert.sameValue(result[1].length, 9);
for (var i = 0; i < 9; i++) {
	assert.sameValue(result[1][i], ["x", "r", "r"][i % 3] + ":default", "conversion " + i);
}

// A variable that a closure reads takes string constants at its start without prepends in place.
function captured(start) {
	var r = start;
	function get() { return r; }
	for (var i = 0; i < 40; i++) {
		r = "x" + r;
		r = "y" + r;
	}
	return get();
}
assert.sameValue(captured(undefined), "yx".repeat(40) + "undefined");
assert.sameValue(captured(1.5), "yx".repeat(40) + "1.5");
assert.sameValue(captured(long), "yx".repeat(40) + long);

// Values read between prepends keep their values.
function readsBetween() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r = "x" + r;
		kept.push(r);
	}
	return kept;
}
result = readsBetween();
for (var i = 0; i < 40; i++) {
	assert.sameValue(result[i], "x".repeat(i + 1) + long, "read " + i);
}

// Values read in loops without prepends, which need no seal of their own, keep their values when
// later prepends extend the variable in place.
function sealedLoops() {
	var r = long, kept = [];
	outer: for (var i = 0; i < 6; i++) {
		r = "x" + r;
		for (var j = 0; j < 3; j++) {
			kept.push(r);
			if (j === 1) {
				continue outer;
			}
		}
	}
	for (var k = 0; k < 3; k++) {
		try {
			for (var j = 0; j < 2; j++) {
				kept.push(r);
			}
		} finally {
			r = "f" + r;
		}
	}
	return kept;
}
result = sealedLoops();
assert.sameValue(result.length, 18);
for (var i = 0; i < 6; i++) {
	assert.sameValue(result[2 * i], "x".repeat(i + 1) + long, "labeled continue " + i);
	assert.sameValue(result[2 * i + 1], "x".repeat(i + 1) + long, "labeled continue " + i);
}
for (var k = 0; k < 3; k++) {
	assert.sameValue(result[12 + 2 * k], "f".repeat(k) + "x".repeat(6) + long, "finally " + k);
	assert.sameValue(result[13 + 2 * k], "f".repeat(k) + "x".repeat(6) + long, "finally " + k);
}
function* sealedYields() {
	var r = long;
	for (var i = 0; i < 4; i++) {
		r = "x" + r;
	}
	for (var j = 0; j < 2; j++) {
		yield r;
	}
	for (var i = 0; i < 4; i++) {
		r = "y" + r;
	}
	return r;
}
var yielded = [], step, it = sealedYields();
while (!(step = it.next()).done) {
	yielded.push(step.value);
}
assert.sameValue(yielded.join(), ["xxxx" + long, "xxxx" + long].join());
assert.sameValue(step.value, "yyyyxxxx" + long);

// A read that comes before the prepends in a loop keeps the value that it read, and the prepends
// still add to the start.
function readBefore(s) {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		kept.push(r);
		r = "<" + r;
		r = s + r;
	}
	return kept;
}
result = readBefore("ab");
for (var i = 0; i < 40; i++) {
	assert.sameValue(result[i], "ab<".repeat(i) + long, "read before " + i);
}

// Variables of blocks that do not overlap: one is read before its prepends, and a closure reads
// the other.
function siblings() {
	var kept = [];
	for (var i = 0; i < 2; i++) {
		{
			let r = "<";
			for (var j = 0; j < 2; j++) {
				kept.push(r);
				r = "a" + r;
			}
		}
		{
			let t = "<";
			t = "b" + t;
			kept.push((() => t)());
		}
	}
	return kept;
}
assert.sameValue(siblings().join(), "<,a<,b<,<,a<,b<");

// Runs of prepends and appends to one variable, and additions of the variable to itself.
function alternating() {
	var r = long, kept = [];
	for (var i = 0; i < 12; i++) {
		for (var j = 0; j < 6; j++) {
			r = "(" + r;
			kept.push(r.length);
		}
		for (var j = 0; j < 6; j++) {
			r += ")";
			kept.push(r.length);
		}
		if (i % 4 === 0) {
			r = r + r;
		}
	}
	kept.push(r);
	return kept;
}
var want = long, lengths = [];
for (var i = 0; i < 12; i++) {
	for (var j = 0; j < 6; j++) {
		want = ["(", want].join("");
		lengths.push(want.length);
	}
	for (var j = 0; j < 6; j++) {
		want = [want, ")"].join("");
		lengths.push(want.length);
	}
	if (i % 4 === 0) {
		want = [want, want].join("");
	}
}
result = alternating();
assert.sameValue(result.slice(0, 144).join(), lengths.join());
assert.sameValue(result[144], want);

// The variable is read after the value, which may assign to it.
function assigningValue() {
	var r = long;
	r = ">" + r;
	for (var i = 0; i < 20; i++) {
		r = (r = "<" + r, "y") + r;
	}
	return r;
}
assert.sameValue(assigningValue(), "y<".repeat(20) + ">" + long);
