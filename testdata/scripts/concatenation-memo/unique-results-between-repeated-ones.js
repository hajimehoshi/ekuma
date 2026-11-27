// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Concatenations with contents that seldom repeat give the right strings, as do the repeated ones around them"
---*/

function piece(s) { return (s + "!").slice(0, -1); }

var p = piece("repeated-"), q = piece("contents");
function repeated() {
	for (var i = 0; i < 2000; i++) {
		assert.sameValue(p + q, "repeated-contents");
	}
}

repeated();
var u = piece("unique-");
for (var i = 0; i < 70000; i++) {
	var s = u + i;
	assert.sameValue(s.length, 7 + String(i).length);
	assert.sameValue(s.slice(7), String(i));
	if (i % 1000 === 0) {
		assert.sameValue(p + q, "repeated-contents");
	}
}
repeated();
assert.sameValue(u + 69999, "unique-69999");
