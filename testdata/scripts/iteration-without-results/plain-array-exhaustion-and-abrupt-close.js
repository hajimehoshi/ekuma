// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array exhaustion skips return, and abrupt close preserves the original error"
---*/

const prototype = Object.getPrototypeOf([][Symbol.iterator]());
const originalError = {};
let closes = 0;
function installReturn() {
	Object.defineProperty(prototype, "return", {
		configurable: true,
		get() {
			closes++;
			throw new RangeError();
		}
	});
}

try {
	let sum = 0;
	for (const value of [2, 3]) {
		sum += value;
		installReturn();
	}
	assert.sameValue(sum, 5);
	assert.sameValue(closes, 0);
	delete prototype.return;

	let caught;
	try {
		for (const value of [2, 3]) {
			installReturn();
			throw originalError;
		}
	} catch (error) {
		caught = error;
	}
	assert.sameValue(caught, originalError);
	assert.sameValue(closes, 1);
} finally {
	delete prototype.return;
}
