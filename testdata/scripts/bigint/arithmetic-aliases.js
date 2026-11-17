// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt arithmetic preserves retained operands"
flags: [noStrict]
---*/

let x = -(1n << 120n);
const saved = [], text = [];
for (let i = 0; i < 32; i++) {
	saved.push(x);
	text.push(String(x));
	x = x * -3n + 1n;
}
assert.sameValue(saved.every((value, i) => String(value) === text[i]), true);
