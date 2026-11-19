// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "block-scoped locals of a script that a closure assigns while they are in use"
---*/

var r;

{ let b = 1; let g = () => { b = 10; return 0; }; r = b + g(); }
assert.sameValue(r, 1, "operand");

var total = 0;
for (let i = 0; i < 3; i++) { const next = () => ++i; total += i * next(); }
assert.sameValue(total, 6, "loop variable");

{ let s = "a"; const add = () => { s += "b"; return "c"; }; r = s + add(); }
assert.sameValue(r, "ac", "string operand");

{ let o = {}; const orig = o; let f = () => { o = {}; return 1; }; o.x = f(); r = orig.x; }
assert.sameValue(r, 1, "member object");

{ let a = [0, 0]; let i = 0; let g = () => { i = 1; return 5; }; a[i] = g(); r = a.join(); }
assert.sameValue(r, "5,0", "computed key");
