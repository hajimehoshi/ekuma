// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "prototype filled from the end"
flags: [noStrict]
---*/

var p = {}; p[99] = 0; p.m = function () { return "m"; };
var o = Object.create(p);
function f() { return o.m() + o[50]; }
var r = [f()];
for (var i = 98; i >= 0; i--) p[i] = i;
r.push(f(), f());
assert.sameValue(String(r.join()), "mundefined,m50,m50");
