// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a block-level function of sloppy code gets no global variable on a non-extensible global object"
flags: [noStrict]
---*/

var r = [];
Object.preventExtensions(globalThis);
$262.evalScript("{ function g() {} r.push(typeof g); } r.push(typeof g);");
assert.sameValue(r.join(), "function,undefined");
