// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments of a sloppy function"
flags: [noStrict]
---*/

// The arguments beyond the parameters outlive the registers they arrived in.
function f(a) {
	a = 3;
	var args = f.arguments;
	var first = args[0];
	args[0] = 9;
	return [args.length, first, args[1], a, args.callee === f, args === f.arguments].join();
}
function r(n) { var inner = n ? r(n - 1) : ""; return r.arguments[0] + inner; }
assert.sameValue(String([String(f.arguments), f(1, 2), r(2)].join("|")), "null|2,3,2,3,true,false|210");
