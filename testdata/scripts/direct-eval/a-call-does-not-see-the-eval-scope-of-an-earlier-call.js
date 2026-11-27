// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a call does not see the eval scope of an earlier call"
flags: [noStrict]
---*/

var x = "global";

function outer(declare, read) {
	eval(declare);
	function f() {
		function h() {
			return eval(read);
		}
		return h();
	}
	return f();
}

assert.sameValue(outer("", "x"), "global");
assert.sameValue(outer("var x = 'local'", "x"), "local");
