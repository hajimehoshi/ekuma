// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "the strings saved while prepending to a non-ASCII string keep their contents after the string grows"
flags: [noStrict]
---*/

const chunk = "é😀";
let s = "", saved = [];
for (let i = 0; i < 100; i++) {
	s = chunk + s;
	assert.sameValue(s.length, 3 * (i + 1));
	saved.push(s);
}
for (let i = saved.length - 1; i >= 0; i--) {
	assert.sameValue(saved[i].length, 3 * (i + 1));
	assert.sameValue(saved[i].charCodeAt(3 * i + 2), 0xDE00);
}
assert.sameValue(saved[39], "é😀".repeat(40));
