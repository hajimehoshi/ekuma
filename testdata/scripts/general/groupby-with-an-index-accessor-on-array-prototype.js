// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "groupBy with an index accessor on Array.prototype"
flags: [noStrict]
---*/

Object.defineProperty(Array.prototype, "0", { get() { throw 1; }, set(v) { throw 2; } });
var m = Map.groupBy(["a", "b"], () => "k");
var o = Object.groupBy(["a"], () => "k");
assert.sameValue(String([m.get("k").length, Object.getOwnPropertyDescriptor(m.get("k"), "1").value, o.k.length].join()), "2,b,1");
