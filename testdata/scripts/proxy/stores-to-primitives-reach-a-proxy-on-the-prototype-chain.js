// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "stores to primitives reach a proxy on the prototype chain"
flags: [noStrict]
---*/

var log = "";
Object.setPrototypeOf(Number.prototype, new Proxy({}, {
	set(t, k, v, r) { log += k + ":" + typeof r + " "; return true; },
}));
(1).x = 1;
(1)[2] = 1;
Object.setPrototypeOf(Number.prototype, Object.prototype);
assert.sameValue(String(log), "x:number 2:number ");
