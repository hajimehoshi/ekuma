// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "holes"
flags: [noStrict]
---*/

var a = Object.setPrototypeOf([10, , 30], {1: 0});
var keys = [];
for (var k in a) keys.push(k);
assert.sameValue(String(keys.join()), "0,2,1");
