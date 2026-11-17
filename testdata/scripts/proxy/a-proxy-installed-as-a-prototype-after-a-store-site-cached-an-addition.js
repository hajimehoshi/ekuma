// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a proxy installed as a prototype after a store site cached an addition"
flags: [noStrict]
---*/

var log = "";
function g(o) { o.y = 1; }
var proto = {};
g(Object.create(proto));
Object.setPrototypeOf(proto, new Proxy({}, { set(t, k, v, r) { log += "set:" + k; return true; } }));
var o = Object.create(proto);
g(o);
assert.sameValue(String(log + " " + Object.hasOwn(o, "y")), "set:y false");
