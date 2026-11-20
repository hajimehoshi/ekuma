// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Default sort coerces both operands of each comparison"
---*/

for (const method of ["sort", "toSorted"]) {
	let calls = 0;
	const a = { toString() { ++calls; return "a"; } };
	const b = { toString() { ++calls; return "b"; } };
	const sorted = [b, a, b][method]();
	assert.sameValue(sorted[0], a);
	assert.sameValue(calls % 2, 0);

	const symbol = Symbol();
	assert.sameValue([symbol][method]()[0], symbol);

	const sentinel = {};
	const throwing = { toString() { throw sentinel; } };
	let caught;
	try {
		[throwing, "x"][method]();
	} catch (e) {
		caught = e;
	}
	assert.sameValue(caught, sentinel);
}
