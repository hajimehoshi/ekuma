// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "template literals"
flags: [noStrict]
---*/

var log = [];
var a = {toString() { log.push("a"); return "A"; }};
var b = {toString() { log.push("b"); return "B"; }, valueOf() { return "ignored"; }};
function f(x) { log.push("f" + x); return x; }
var t = `<${a}|${f(1)}|${b}${''}${f(2)}>`;
var out = "";
for (var i = 0; i < 50; i++) out = `${out}${i % 10}`;
assert.sameValue(String([t, log.join(), out.length, out.slice(-5), `${''}`.length, `x`].join(" ")), "\u003cA|1|B2\u003e a,f1,b,f2 50 56789 0 x");
