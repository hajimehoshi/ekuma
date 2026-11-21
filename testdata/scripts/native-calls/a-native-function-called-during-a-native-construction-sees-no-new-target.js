// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a native function that a native constructor calls while it constructs sees no new.target, and the construction still constructs"
---*/

// ToPrimitive calls valueOf from the constructor.
const fromNative = new Number({ valueOf: Number });
assert.sameValue(typeof fromNative, "object");
assert.sameValue(fromNative.valueOf(), 0);

let inner;
const fromScript = new Number({
	valueOf() {
		inner = Number();
		return 1;
	},
});
assert.sameValue(typeof inner, "number");
assert.sameValue(typeof fromScript, "object");
assert.sameValue(fromScript.valueOf(), 1);
