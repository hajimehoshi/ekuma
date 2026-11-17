// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments passed on past a parameter default"
flags: [noStrict]
---*/

function g() { return arguments[arguments.length - 1]; }
function f(a = 1) { return g.apply(null, arguments); }
function h(a, b = a) { return g(...arguments); }
var o = { set s(a = 1) { this.r = g(...arguments); } }; o.s = undefined;
assert.sameValue(String([f(undefined), h(1, undefined), o.r].join()), ",,");
