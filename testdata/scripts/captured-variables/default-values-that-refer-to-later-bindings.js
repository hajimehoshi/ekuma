// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "default values that refer to later bindings"
flags: [noStrict]
---*/

function f(a = () => b, b = 2) { return a(); }
var r; try { throw {}; } catch ({c = () => d, d = 3}) { r = c(); }
assert.sameValue(String(f() + r), "5");
