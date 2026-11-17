// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "function declarations instantiated together"
flags: [noStrict]
---*/

function f() { { function g() { return typeof h; } function h() {} return g(); } } assert.sameValue(String(f()), "function");
