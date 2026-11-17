// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "call through a bound function"
flags: [onlyStrict]
---*/

var c = Function.prototype.call.bind(f, "t");
function f(n) { return n === 0 ? this : c(n - 1); }
assert.sameValue(String(c(1e6)), "t");
