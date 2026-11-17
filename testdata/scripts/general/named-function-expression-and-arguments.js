// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "named function expression and arguments"
flags: [noStrict]
---*/

var f = function fact(n) { return n <= 1 ? 1 : n * fact(n - 1); }; assert.sameValue(String((function() { return arguments.length + f(arguments[0]); })(4, 5)), "26");
