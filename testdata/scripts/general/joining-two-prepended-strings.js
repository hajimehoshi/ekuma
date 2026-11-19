// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "two prepended strings joined keep the order of both"
flags: [noStrict]
---*/

let a = "", b = "!";
const parts = [];
for (let i = 0; i < 32; i++) {
	a = "ab" + a;
}
for (let i = 0; i < 40; i++) {
	const c = String(i % 10);
	b = c + b;
	parts.push(c);
}
const result = a + b;
assert.sameValue(result, "ab".repeat(32) + parts.reverse().join("") + "!");
