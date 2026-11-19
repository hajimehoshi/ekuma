// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.assign throws a TypeError for a read-only property that the target inherits"
flags: [noStrict]
---*/

Object.defineProperty(Object.prototype, "b", {value: 0, configurable: true});
let caught;
try {
	Object.assign({}, {a: 1, b: 2});
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof TypeError, true);
