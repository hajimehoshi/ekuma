// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a proxy installed as a prototype after a load site cached a miss"
flags: [noStrict]
---*/

function f(o) { return o.x; }
var proto = {};
var o = Object.create(proto);
var before = f(o);
Object.setPrototypeOf(proto, new Proxy({}, { get() { return "trap"; } }));
assert.sameValue(String(before + " " + f(o)), "undefined trap");
