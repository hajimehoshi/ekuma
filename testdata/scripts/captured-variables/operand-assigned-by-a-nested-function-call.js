// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "operand assigned by a call of a nested function"
flags: [noStrict]
---*/

function f(a) { function g() { a = 1; return 0; } return a + g(); } assert.sameValue(String(f(5)), "5");
