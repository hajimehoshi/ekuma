// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "eval declarations in parameter defaults shadow the name of a function expression"
flags: [noStrict]
---*/

assert.sameValue(String([
	(function f(x = eval("var f = 3"), y = () => f) { return typeof f + y(); })(),
	(function f(x = () => f, y = eval("var f = 4")) { return x(); })(),
	(function f(x = eval("1"), y = () => typeof f) { return y(); })(),
].join()), "number3,4,function");
