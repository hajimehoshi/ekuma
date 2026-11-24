// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.prototype.toString passes a wrapper of a primitive to a getter"
flags: [noStrict, noRepeat]
noRepeatReason: "it adds accessors to built-in prototypes for good"
---*/

Object.defineProperty(Number.prototype, Symbol.toStringTag, {
	get() { "use strict"; return typeof this; },
});
assert.sameValue(String(Object.prototype.toString.call(1)), "[object object]");
