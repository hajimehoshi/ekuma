// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Partial array destructuring propagates an exception from the return getter"
flags: [noRepeat]
noRepeatReason: "it adds an accessor to a built-in prototype for good"
---*/

const prototype = Object.getPrototypeOf([][Symbol.iterator]());
Object.defineProperty(prototype, "return", {
	get() { throw this.next().value; },
});
let caught;
try {
	const [first] = [10, 20];
} catch (e) {
	caught = e;
}
assert.sameValue(caught, 20);
