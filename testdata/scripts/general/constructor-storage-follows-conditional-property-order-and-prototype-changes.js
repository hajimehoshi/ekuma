// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "constructor storage follows conditional property order and prototype changes"
flags: [noStrict]
---*/

function C(reverse) {
	if (reverse) { this.b = 2; this.a = 1; }
	else { this.a = 1; this.b = 2; }
}
function make(reverse) { return new C(reverse); }
let first = make(false), second = make(true);
let order = Object.keys(second);
let seen = [];
C.prototype = { set a(v) { seen.push(v); } };
let third = make(false);
delete second.b;
second.b = 3;
assert.sameValue(String([Object.keys(first), order, Object.keys(second), Object.keys(third), seen,
 Object.getPrototypeOf(third) === C.prototype, first.b, second.b].join("|")), "a,b|b,a|a,b|b|1|true|2|3");
