// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "object spread copies enumerable data properties into an independent object"
flags: [noStrict]
---*/

var src = {a: 1, b: 2}, c = {...src};
c.a = 3; c.x = 4; src.y = 5;
var f = {...Object.freeze({p: 1, q: 2})}; f.p = 6; f.r = 7;
var n = {...Object.defineProperty({u: 1, v: 2}, "u", {enumerable: false})};
assert.sameValue(String([src.a, "x" in src, "y" in c, Object.keys(c), Object.isFrozen(f), Object.keys(f), f.p, Object.keys(n)].join("|")), "1|false|false|a,b,x|false|p,q,r|6|v");
