// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "apply with an array or no array a million deep"
flags: [onlyStrict]
---*/

function f(n, acc) { return n === 0 ? acc : n % 2 ? f.apply(null, [n - 1, acc + 1]) : g.apply(n - 1); }
function g() { return f(this, 0); }
assert.sameValue(String(f(1e6, 0)), "1");
