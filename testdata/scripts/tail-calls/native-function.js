// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "native function"
flags: [onlyStrict]
---*/

function f(n) { return n === 0 ? Math.max(n, 7) : Reflect.apply(g, null, [n]); }
function g(n) { return f(n - 1); }
assert.sameValue(String(f(50)), "7");
