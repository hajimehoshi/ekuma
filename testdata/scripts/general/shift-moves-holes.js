// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "shift moves holes"
flags: [noStrict]
---*/

var a = [0, 1, , 3]; for (var i = 4; i < 40; i++) a.push(i);
a.shift();
assert.sameValue(String([a.length, 0 in a, 1 in a, 2 in a, a[2]].join()), "39,true,false,true,3");
