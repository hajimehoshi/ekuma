// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "with scope resolved before a destructuring step"
flags: [noStrict]
---*/

var o = {a: 0, b: 0, c: 0, d: 0}; var a, b, c, d;
var src = {get p() { delete o.a; return 1; }};
function* g() { delete o.b; yield 2; delete o.c; yield 3; }
with (o) { ({p: a} = src); var [b, ...c] = g(); ({...d} = {get q() { delete o.d; }}); }
assert.sameValue(String([o.a, o.b, o.c, typeof o.d, a, b, c].join()), "1,2,3,object,,,");
