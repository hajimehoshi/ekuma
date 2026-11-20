// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Array.from closes the iterator when its mapper throws"
---*/

const prototype = Object.getPrototypeOf([][Symbol.iterator]());
const error = {};
let nextValue;
let caught;
try {
	Array.from([10, 20], () => {
		prototype.return = function () {
			nextValue = this.next().value;
			throw new Error();
		};
		throw error;
	});
} catch (e) {
	caught = e;
}
assert.sameValue(caught, error);
assert.sameValue(nextValue, 20);
