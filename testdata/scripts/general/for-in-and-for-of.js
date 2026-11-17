// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "for in and for of"
flags: [noStrict]
---*/

var o = {x: 1, y: 2}, ks = []; for (var k in o) ks.push(k);
var s = 0; for (const v of [1, 2, 3]) s += v; assert.sameValue(String(ks.join() + s), "x,y6");
