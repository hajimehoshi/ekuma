// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "functions without upvalues have separate prototypes and dynamic scopes"
flags: [noStrict]
---*/

function make() { return function callback() {}; }
var first = make();
var second = make();
assert.sameValue(first.prototype.constructor, first);
assert.sameValue(second.prototype.constructor, second);
assert.sameValue(first === second, false);

function inScope(scope) {
	with (scope) { return function () { return value; }; }
}
var one = inScope({value: 1});
var two = inScope({value: 2});
assert.sameValue(one(), 1);
assert.sameValue(two(), 2);
