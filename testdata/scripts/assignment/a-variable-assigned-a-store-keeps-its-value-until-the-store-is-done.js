// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a variable assigned the value of a property store keeps its old value until the store is done, as a catch or finally block, a closure or the arguments of its function see it"
flags: [noStrict]
---*/

var thrower = {set p(v) { throw new Error("thrown"); }};

assert.sameValue((function () {
	var x = 1;
	try {
		x = thrower.p = 2;
	} catch (e) {
		return x;
	}
})(), 1, "a catch block");

var seen;
function inFinally() {
	var x = 1;
	try {
		x = thrower.p = 2;
	} finally {
		seen = x;
	}
}
try {
	inFinally();
} catch (e) {
}
assert.sameValue(seen, 1, "a finally block");

assert.sameValue((function () {
	var x = 1, read;
	var o = {set p(v) { read = x; }};
	x = o.p = 2;
	return [read, x].join();
})(), "1,2", "a closure");

var argument;
var reader = {set p(v) { argument = withParameter.arguments[0]; }};
function withParameter(x) {
	x = reader.p = 2;
	return x;
}
assert.sameValue([withParameter(1), argument].join(), "2,1", "the arguments of the function");

assert.sameValue((function () {
	var x = 1, a = [0];
	x = a[0] = x + 5;
	return [x, a[0]].join();
})(), "6,6", "a store that reads the variable");
