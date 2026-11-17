// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "instanceof with proxies"
flags: [noStrict]
---*/

var log = [];
function F() {}
var P = new Proxy(F, { get(t, k, r) { log.push(String(k)); return Reflect.get(t, k, r); } });
var q = new Proxy({}, { getPrototypeOf(t) { log.push("getPrototypeOf"); return F.prototype; } });
assert.sameValue(String((new F() instanceof P) + " " + (q instanceof F) + " " + log.join()), "true true Symbol(Symbol.hasInstance),prototype,getPrototypeOf");
