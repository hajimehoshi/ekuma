// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "switch compares the discriminant strictly, keeps it when a case test reassigns the variable, and runs the tests in order"
---*/

function kind(x) {
	switch (x) {
	case 0: return "zero";
	case 1: return "one";
	case -1: return "minus one";
	case "1": return "string one";
	case NaN: return "NaN";
	default: return "other";
	}
}
assert.sameValue(kind(0), "zero");
assert.sameValue(kind(-0), "zero");
assert.sameValue(kind(1), "one");
assert.sameValue(kind(-1), "minus one");
assert.sameValue(kind("1"), "string one");
assert.sameValue(kind(1n), "other");
assert.sameValue(kind(NaN), "other");
assert.sameValue(kind(true), "other");
assert.sameValue(kind(new Number(1)), "other");

function negativeZero(x) {
	switch (x) {
	case -0: return "zero";
	}
	return "other";
}
assert.sameValue(negativeZero(0), "zero");

// A case test that reassigns the variable does not change the discriminant.
assert.sameValue((function() {
	var x = 1;
	switch (x) {
	case (x = 2): return "two";
	case 1: return "one, x = " + x;
	}
	return "none";
})(), "one, x = 2");

assert.sameValue((function() {
	var x;
	switch (x = 1) {
	case (x = 2): return "two";
	case 1: return "one";
	}
})(), "one");

assert.sameValue((function() {
	var x = "a";
	function set() { x = "b"; return "b"; }
	switch (x) {
	case set(): return "b";
	case "a": return "a, x = " + x;
	}
	return "none";
})(), "a, x = b");

// The tests run in order up to the match, the tests after a default in the middle run before
// the default, and a match falls through.
function trace(x) {
	var log = [];
	function t(v) { log.push(v); return v; }
	switch (x) {
	case t(1): log.push("body 1");
	default: log.push("default");
	case t(2): log.push("body 2"); break;
	case t(3): log.push("body 3");
	}
	return log.join();
}
assert.sameValue(trace(1), "1,body 1,default,body 2");
assert.sameValue(trace(2), "1,2,body 2");
assert.sameValue(trace(3), "1,2,3,body 3");
assert.sameValue(trace(4), "1,2,3,default,body 2");
