// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "length"
flags: [noStrict]
---*/

// The length of a typed array keeps following its getter and own properties
// after repeated reads of the same site.
var a = new Uint8Array(4), r = [];
function len(x) { return x.length; }
for (var i = 0; i < 10; i++) len(a);
r.push(len(a));
Object.defineProperty(Object.getPrototypeOf(Uint8Array.prototype), "length", {get() { return 7; }});
r.push(len(a));
Object.defineProperty(a, "length", {value: 9});
r.push(len(a));
assert.sameValue(String(r.join()), "4,7,9");
