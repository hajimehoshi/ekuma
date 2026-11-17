// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "unshift into an array with room"
flags: [noStrict]
---*/

var a = []; for (var i = 0; i < 10; i++) a.unshift(i, -i);
a.push(99); a.unshift(); assert.sameValue(String(a.join()), "9,-9,8,-8,7,-7,6,-6,5,-5,4,-4,3,-3,2,-2,1,-1,0,0,99");
