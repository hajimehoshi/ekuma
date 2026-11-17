// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "spread arguments a million deep"
flags: [onlyStrict]
---*/

function f(n) { return g(...arguments); }
function g(n, a, b) { return n === 0 ? a + b : f(n - 1, a, b); }
assert.sameValue(String(f(1e6, 1, 2)), "3");
