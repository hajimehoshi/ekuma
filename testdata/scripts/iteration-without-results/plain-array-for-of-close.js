// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A break closes the array iterator at its current position"
---*/

const prototype = Object.getPrototypeOf([][Symbol.iterator]());
let nextValue;
for (const value of [10, 20]) {
	prototype.return = function () {
		nextValue = this.next().value;
		return {};
	};
	break;
}
assert.sameValue(nextValue, 20);
