// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "apply forwarding arguments a million deep"
flags: [onlyStrict]
---*/

function f(n, acc) { return g.apply(this, arguments); }
function g(n, acc, x) { return n === 0 ? acc + x : f(n - 1, acc + 1, x); }
assert.sameValue(String(f(1e6, 0, "x")), "1000000x");
