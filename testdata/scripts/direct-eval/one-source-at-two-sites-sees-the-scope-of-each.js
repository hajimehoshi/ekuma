// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "one source at two sites sees the scope of each"
flags: [noStrict]
---*/

var src = "var v = typeof x == 'undefined' ? 'none' : x; v";
function f(x) {
	let r = [eval(src)];
	{
		let x = "block";
		r.push(eval(src));
	}
	r.push(v);
	return r.join();
}
assert.sameValue(String(f("param")), "param,block,block");
