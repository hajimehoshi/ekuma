// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "BigInt steps retain prefix and postfix values"
flags: [noStrict]
---*/

assert.sameValue([63n, 64n, 255n, 256n, 257n].every(bits => {
	let power = 1n << bits, positive = power - 1n, negative = -power;
	let oldPositive = positive++, oldNegative = negative--;
	let nextPositive = ++positive, nextNegative = --negative;
	positive--;
	negative++;
	return oldPositive === power - 1n && oldNegative === -power
		&& nextPositive === power - (-1n) && nextNegative === -power - 2n
		&& positive === power && negative === -power - 1n;
}), true);
