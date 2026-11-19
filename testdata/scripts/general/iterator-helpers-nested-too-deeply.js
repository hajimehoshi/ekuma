// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a step of a chain of iterator helpers too long for the native call depth throws a RangeError"
flags: [noStrict]
---*/

var it = [1].values();
for (let i = 0; i < 20000; i++) it = it.map(Math.abs);
var caught;
try {
	it.next();
} catch (error) {
	caught = error;
}
assert.sameValue(caught instanceof RangeError, true);
