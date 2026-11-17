// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "eval declarations shadow the name of a function expression"
flags: [noStrict]
---*/

assert.sameValue(String([
	(function f() { var before = typeof f; eval("var f = 1"); return before + typeof f; })(),
	(function f() { eval("function f() { return 1; }"); return f(); })(),
	(function f() { eval("var f"); f = 2; return f; })(),
].join()), "functionnumber,1,2");
