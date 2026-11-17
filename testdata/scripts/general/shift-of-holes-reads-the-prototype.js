// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "shift of holes reads the prototype"
flags: [noStrict]
---*/

Array.prototype[2] = "p";
var a = [0, 1, , 3]; for (var i = 4; i < 40; i++) a.push(i);
a.shift();
assert.sameValue(String([a.length, a.hasOwnProperty(1), a[1], 38 in a].join()), "39,true,p,true");
