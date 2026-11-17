// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt products preserve operands and results across bounded and large multiplication"
flags: [noStrict]
---*/

const retained = [];
for (const bits of [63n, 127n, 128n, 4096n]) {
	const power = 1n << bits;
	const x = power - 1n, y = power + 1n;
	const square = x * x;
	const negative = x * -y;
	retained.push([x, y, square, negative, power, bits]);
}
for (const [x, y, square, negative, power, bits] of retained) {
	assert.sameValue(x, power - 1n);
	assert.sameValue(y, power + 1n);
	assert.sameValue(square, (1n << (bits + bits)) - (power << 1n) + 1n);
	assert.sameValue(negative, 1n - (1n << (bits + bits)));
}

// Unequal operand lengths fill the bounded result buffer.
assert.sameValue(((1n << 191n) - 1n) * 3n, (3n << 191n) - 3n);
assert.sameValue((1n << 63n) * -1n, -9223372036854775808n);
assert.sameValue((1n << 255n) * 0n, 0n);
