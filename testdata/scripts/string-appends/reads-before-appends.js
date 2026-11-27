// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A read of a variable that comes before the appends to it in the code keeps the value that it read"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function check(name, kept, first) {
	assert.sameValue(kept.length, 40, name + " count");
	for (var i = 0; i < 40; i++) {
		assert.sameValue(kept[i], long + "x".repeat(first + i), name + " " + i);
	}
}

function loop() {
	var r = long, kept = [];
	for (var i = 0; i < 41; i++) {
		if (i > 0) {
			kept.push(r);
		}
		r += "x";
	}
	return kept;
}
check("loop", loop(), 1);

function handler() {
	var r = long, kept = [];
	for (var i = 0; i < 41; i++) {
		try {
			if (i > 0) {
				throw 0;
			}
		} catch (e) {
			kept.push(r);
		}
		r += "x";
	}
	return kept;
}
check("handler", handler(), 1);

function loopCondition() {
	var r = long, kept = [];
	r += "x";
	while (r.length < long.length + 41) {
		kept.push(r);
		r += "x";
	}
	return kept;
}
check("loop condition", loopCondition(), 1);

// Variables of blocks that do not overlap, one of which is read before its appends.
function siblings(s) {
	var kept = [];
	for (var i = 0; i < 2; i++) {
		{
			let r = long;
			r += "a";
			r += s;
			kept.push(r);
		}
		{
			let t = long;
			for (var j = 0; j < 2; j++) {
				kept.push(t);
				t += "x";
				t += s;
			}
		}
	}
	return kept;
}
var sib = siblings("b");
assert.sameValue(sib.length, 6);
for (var i = 0; i < 2; i++) {
	assert.sameValue(sib[3 * i], long + "ab", "first block " + i);
	assert.sameValue(sib[3 * i + 1], long, "second block before appends " + i);
	assert.sameValue(sib[3 * i + 2], long + "xb", "second block after an append " + i);
}

// A continue that leaves the block of the variable.
function leaving() {
	var kept = [];
	for (var i = 0; i < 4; i++) {
		let t = long;
		for (var j = 0; j < 40; j++) {
			t += "x";
		}
		if (i % 2) {
			t += "y";
			kept.push(t);
			continue;
		}
		kept.push(t);
	}
	return kept;
}
var left = leaving();
for (var i = 0; i < 4; i++) {
	assert.sameValue(left[i], long + "x".repeat(40) + (i % 2 ? "y" : ""), "continue " + i);
}

