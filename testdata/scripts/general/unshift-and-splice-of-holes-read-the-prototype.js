// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "unshift and splice of holes read the prototype"
flags: [noStrict]
---*/

Array.prototype[2] = "p";
var a = [0, 1, , 3]; a.unshift(9);
var b = [0, 1, , 3, 4]; var r = b.splice(1, 2, 7);
var c = [0, 1, 2, , 4]; c.splice(0, 1); c.splice(1, 0, "x", "y");
assert.sameValue(String([a.length, a.hasOwnProperty(3), a[3], r.length, r.hasOwnProperty(1), b.join(), c.length, c.hasOwnProperty(4), c.hasOwnProperty(2), c[2]].join()), "5,true,p,2,true,0,7,3,4,6,true,true,y");
