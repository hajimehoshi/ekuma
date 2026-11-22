// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Numeric property lookup stops at a typed array before a proxy on its prototype chain"
---*/

const view = new Uint8Array(1);
Object.setPrototypeOf(view, new Proxy({}, {
	get() { throw new Test262Error("unexpected lookup beyond the typed array"); }
}));
const object = Object.create(view);
assert.sameValue(object["-1"], undefined);
