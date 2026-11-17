// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "compiling a RegExp from a literal leaves the other objects of the literal alone"
flags: [noStrict]
---*/

function f() { return /a/g; }
var r1 = f(), r2 = f();
r1.compile("b", "i");
assert.sameValue(String([r1 !== r2, r1.source, r1.flags, r2.source, r2.flags, r2.test("A"), f().test("a")].join()), "true,b,i,a,g,false,true");
