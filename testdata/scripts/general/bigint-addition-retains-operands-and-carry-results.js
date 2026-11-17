// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt addition retains operands and carry results"
flags: [noStrict]
---*/

assert.sameValue([63n, 64n, 127n, 255n, 256n, 257n].every(bits => {
	let power = 1n << bits, x = power - 1n;
	let carry = x + 1n, doubled = x + x, negative = -x + -x;
	let retained = [x, carry, doubled, negative];
	let next = carry + carry;
	for (let i = 0; i < 20; i++) next = next + carry;
	return retained[0] === power - 1n && retained[1] === power
		&& retained[2] === x << 1n && retained[3] === -(x << 1n)
		&& next === power * 22n && x + -x === 0n
		&& -x + -1n === -power;
}), true);
