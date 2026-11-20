// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Strict assignment resolves its target before unary coercion creates a global"
flags: [onlyStrict]
---*/

const o = { valueOf() { globalThis.createdDuringCoercion = 12; return 3; } };
let caught;
try {
	createdDuringCoercion = -o;
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof ReferenceError, true);
assert.sameValue(globalThis.createdDuringCoercion, 12);
