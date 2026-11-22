// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "slice uses the species getter inherited by Array"
flags: [noStrict]
---*/

class Other extends Array {}
delete Array[Symbol.species];
Object.defineProperty(Function.prototype, Symbol.species, {
	get() {
		assert.sameValue(this, Array);
		return Other;
	},
});
assert.sameValue(Object.getPrototypeOf([1].slice()), Other.prototype);
