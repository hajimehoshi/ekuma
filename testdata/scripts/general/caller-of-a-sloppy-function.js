// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "caller of a sloppy function"
flags: [noStrict]
---*/

function f() { return f.caller; }
function g() { return f(); }
var a = () => f();
var o = { m() { return f(); } };
assert.sameValue(String([f.caller === null, f() === null, g() === g, a() === a, o.m() === o.m, f.caller === null].join()), "true,true,true,true,true,true");
