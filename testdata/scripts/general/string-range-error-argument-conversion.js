// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "String.fromCodePoint and String.prototype.repeat convert an invalid argument only once"
---*/

for (const call of [String.fromCodePoint, n => "x".repeat(n)]) {
	let conversions = "";
	const arg = {
		valueOf() {
			conversions += "valueOf";
			return -1;
		},
		toString() {
			conversions += "toString";
			throw new TypeError();
		},
	};
	let error;
	try {
		call(arg);
	} catch (e) {
		error = e;
	}
	assert.sameValue(error instanceof RangeError, true);
	assert.sameValue(conversions, "valueOf");
}
