// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "array filled from the end keeps the attributes of its elements"
flags: [noStrict]
---*/

var a = [];
Object.defineProperty(a, 150, { value: "ro", writable: false, enumerable: true, configurable: true });
Object.defineProperty(a, 120, { get: function () { return "g"; }, enumerable: false, configurable: true });
for (var i = 199; i >= 0; i--) if (i !== 150 && i !== 120) a[i] = i;
a[150] = "changed";
var k = Object.keys(a);
assert.sameValue(String([a[150], Object.getOwnPropertyDescriptor(a, 150).writable, a[120], k.length, k.indexOf("120"), a.length].join("|")), "ro|false|g|199|-1|200");
