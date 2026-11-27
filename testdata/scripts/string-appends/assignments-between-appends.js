// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Assignments of other kinds between appends to a variable give it their values"
---*/

var long = "0123456789abcdefghijklmnopqrstuvwxyz";

function assigned() {
	var r = long, kept = [], k;
	for (var i = 0; i < 40; i++) {
		r += "x";
		kept.push(r);
		switch (i % 4) {
		case 0:
			[r] = [long + i];
			break;
		case 1:
			({ r } = { r: long + "-" + i });
			break;
		case 2:
			for (r of [long + "+" + i]) {
			}
			break;
		case 3:
			for (r in { [long + "*" + i]: 0 }) {
			}
			break;
		}
		r += "z";
		kept.push(r);
	}
	return kept;
}
var kept = assigned();
var r = long, marks = ["", "-", "+", "*"];
for (var i = 0; i < 40; i++) {
	r += "x";
	assert.sameValue(kept[2 * i], r, "append " + i);
	r = long + marks[i % 4] + i + "z";
	assert.sameValue(kept[2 * i + 1], r, "assignment " + i);
}

function finallyBlocks() {
	var r = long, kept = [];
	for (var i = 0; i < 40; i++) {
		try {
			r += "x";
			if (i % 2) {
				throw i;
			}
		} catch (e) {
			r += "c";
		} finally {
			kept.push(r);
		}
	}
	return kept;
}
kept = finallyBlocks();
r = long;
for (var i = 0; i < 40; i++) {
	r += i % 2 ? "xc" : "x";
	assert.sameValue(kept[i], r, "finally " + i);
}

function let_in_loop() {
	var kept = [];
	for (var j = 0; j < 3; j++) {
		let r = long;
		for (var i = 0; i < 40; i++) {
			r += "x";
		}
		kept.push(r);
	}
	return kept;
}
kept = let_in_loop();
for (var j = 0; j < 3; j++) {
	assert.sameValue(kept[j], long + "x".repeat(40), "let " + j);
}
