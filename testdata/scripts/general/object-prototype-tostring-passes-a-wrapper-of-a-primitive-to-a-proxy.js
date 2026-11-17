// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "Object.prototype.toString passes a wrapper of a primitive to a proxy"
flags: [noStrict]
---*/

Object.setPrototypeOf(Boolean.prototype, new Proxy({}, {
	get(t, k, r) { return typeof r; },
}));
assert.sameValue(String(Object.prototype.toString.call(true)), "[object object]");
