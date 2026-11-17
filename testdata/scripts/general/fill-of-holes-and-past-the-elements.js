// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "fill of holes and past the elements"
flags: [noStrict]
---*/

var a = [1, , 3]; a.length = 6; a.fill(0, 1, 5); assert.sameValue(String([a.join(), 5 in a, new Array(3000).fill(2).lastIndexOf(2)].join("|")), "1,0,0,0,0,|false|2999");
