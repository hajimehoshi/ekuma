// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "pop with holes"
flags: [noStrict]
---*/

var a = [, 1, 2]; var b = [1, , ];
Array.prototype[1] = "p";
var r = [a.pop(), a.length, 0 in a, b.pop(), b.length, b.hasOwnProperty(1)];
delete Array.prototype[1];
assert.sameValue(String(r.join("|")), "2|2|false|p|1|false");
