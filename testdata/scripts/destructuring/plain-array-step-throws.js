// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A throwing array element getter finishes the iterator during destructuring"
---*/

const error = {};
let closed = false;
Object.getPrototypeOf([][Symbol.iterator]()).return = function () {
	closed = true;
	return {};
};
Object.defineProperty(Array.prototype, "1", {
	configurable: true,
	get() { throw error; },
});
let caught;
try {
	const [, second] = [0, ,];
} catch (e) {
	caught = e;
} finally {
	delete Array.prototype[1];
}
assert.sameValue(caught, error);
assert.sameValue(closed, false);
