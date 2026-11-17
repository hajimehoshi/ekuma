// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array filled from the end"
flags: [noStrict]
---*/

var a = []; for (var i = 199; i >= 0; i--) { a[i] = i * 2; if (i === 150) a.x = "x"; }
var k = Object.keys(a);
assert.sameValue(String([a.length, a[0], a[17], a[199], k.length, k[0], k[1], k[199], k[200], a.indexOf(300), a.lastIndexOf(2),
	JSON.stringify(Object.getOwnPropertyDescriptor(a, 180))].join("|")), "200|0|34|398|201|0|1|199|x|150|1|{\"value\":360,\"writable\":true,\"enumerable\":true,\"configurable\":true}");
