// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "own keys hide the keys of prototypes"
flags: [noStrict]
---*/

var p2 = {p: 0, 1: 0, length: 0, m: 0};
var p1 = Object.create(p2, {m: {value: 0, enumerable: false}});
var a = Object.setPrototypeOf([10, 20, 30], p1);
a.n = 0;
var keys = [];
for (var k in a) keys.push(k);
assert.sameValue(String(keys.join()), "0,1,2,n,p");
