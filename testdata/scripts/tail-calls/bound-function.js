// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "bound function"
flags: [onlyStrict]
---*/

var h = f.bind("t", 1);
function f(k, n) { return n === 0 ? this + k : h(n - 1); }
assert.sameValue(String(h(1e5)), "t1");
