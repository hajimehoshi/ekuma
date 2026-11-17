// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "closures made by eval code outlive the caller"
flags: [noStrict]
---*/

function make(n) {
	var count = n;
	let step = 10;
	return eval("(function () { count += step; return count; })");
}
var a = make(1), b = make(2);
a(); a(); b();
assert.sameValue(String([a(), b()].join()), "31,22");
