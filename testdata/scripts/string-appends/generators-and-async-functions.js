// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Appends in generators and async functions keep every value that was yielded, awaited or returned"
flags: [async]
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function* yields() {
	var r = long;
	for (var i = 0; i < 40; i++) {
		r += "x";
		yield r;
		// The generator resumes with its variables on the heap.
		r += "y";
	}
	return r;
}
var values = [];
var g = yields();
for (var step = g.next(); !step.done; step = g.next()) {
	values.push(step.value);
}
values.push(step.value);
assert.sameValue(values.length, 41);
var expected = long;
for (var i = 0; i < 40; i++) {
	expected += "x";
	assert.sameValue(values[i], expected, "yield " + i);
	expected += "y";
}
assert.sameValue(values[40], expected);

function* suspended() {
	var r = long;
	for (var i = 0; i < 40; i++) {
		r += "x";
		yield i;
	}
	return r;
}
var it = suspended(), last;
while (!(last = it.next()).done) {
}
assert.sameValue(last.value, long + "x".repeat(40));

async function awaits() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(await r);
		r += await "y";
	}
	return kept;
}
awaits().then(function (kept) {
	var expected = long;
	for (var i = 0; i < 40; i++) {
		expected += "x";
		assert.sameValue(kept[i], expected, "await " + i);
		expected += "y";
	}
}).then($DONE, $DONE);
