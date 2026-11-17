// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "apply with an array-like"
flags: [onlyStrict]
---*/

var log = [];
var args = { get length() { log.push("length"); return 2; }, get 0() { log.push(0); return 3; }, 1: 4 };
function f(a, b) { log.push("f"); return a + b; }
function g() { return f.apply(null, args); }
var r = g();
function h() { return f.apply(null, { get length() { throw "x"; } }); }
var e = (function () { try { return h(); } catch (e) { return e; } })();
assert.sameValue(String(log.join() + " " + r + " " + e), "length,0,f 7 x");
