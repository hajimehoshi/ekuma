// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments passed on after a parameter is declared again"
flags: [onlyStrict]
---*/

function g() { return [...arguments].join(); }
function f1(a) { var a = 5; return g(...arguments); }
function f2(a) { var [a] = [5]; return g.apply(this, arguments); }
function f3(a) { for (var a of [5]); return g(...arguments); }
function f4(a) { for (var a in { 5: 0 }); return g(...arguments); }
function f5(a) { function a() {} return g(...arguments); }
assert.sameValue([f1(1), f2(1), f3(1), f4(1), f5(1)].join("|"), "1|1|1|1|1");
