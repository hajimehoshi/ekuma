// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "apply forwards the parameters and the extra arguments of the running function"
---*/

function g(x, y, z, w) { return this.p + arguments.length + ":" + [x, y, z, w].join(); }
function none() { return g.apply(this, arguments); }
function one(a) { return g.apply(this, arguments); }
function two(a, b) { return g.apply(this, arguments); }
var o = { p: "o", none: none, one: one, two: two };
assert.sameValue(o.none("a", "b", "c"), "o3:a,b,c,");
assert.sameValue(o.one(), "o0:,,,");
assert.sameValue(o.one("d", "e", "f"), "o3:d,e,f,");
assert.sameValue(o.two("g"), "o1:g,,,");
assert.sameValue(o.two("h", "i", "j", "k"), "o4:h,i,j,k");
