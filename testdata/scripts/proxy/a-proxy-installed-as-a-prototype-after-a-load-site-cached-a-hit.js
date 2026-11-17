// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a proxy installed as a prototype after a load site cached a hit"
flags: [noStrict]
---*/

function f(o) { return o.x; }
var proto = {};
var o = Object.create(proto);
o.y = 1;
var mid = Object.create(Object.prototype);
mid.x = "mid";
Object.setPrototypeOf(proto, mid);
var before = f(o);
Object.setPrototypeOf(proto, new Proxy({}, { get(t, k, r) { return k + ":" + (r === o); } }));
assert.sameValue(String(before + " " + f(o)), "mid x:true");
