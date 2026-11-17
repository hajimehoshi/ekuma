// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "recursion"
flags: [noStrict]
---*/

function fib(n) { return n < 2 ? n : fib(n - 1) + fib(n - 2); } assert.sameValue(String(fib(20)), "6765");
