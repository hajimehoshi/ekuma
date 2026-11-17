// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "one source at one site sees the current values"
flags: [noStrict]
---*/

function f(x) {
	var out = [];
	for (let i = 0; i < 3; i++) {
		out.push(eval("x + i"));
	}
	return out.join();
}
assert.sameValue(String([f(1), f(10)].join("/")), "1,2,3/10,11,12");
