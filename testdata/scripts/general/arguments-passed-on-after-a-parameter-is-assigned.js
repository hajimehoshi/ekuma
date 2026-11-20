// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "arguments passed on after a parameter is assigned"
flags: [onlyStrict]
---*/

function g(x) { return x; }
function f(a) { a = 2; return g.apply(null, arguments); }
function h(a) { (() => { a = 2; })(); return g(...arguments); }
assert.sameValue(String([f(1), h(1)].join()), "1,1");

function nestedEval(a) {
	function change() { eval("a = 2"); }
	change();
	return g(...arguments);
}
assert.sameValue(nestedEval(1), 1);
