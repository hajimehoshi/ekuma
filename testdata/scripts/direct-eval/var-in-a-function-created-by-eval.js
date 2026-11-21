// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a function created by eval can declare its own variable over an outer lexical binding"
flags: [noStrict]
---*/

function make() {
	let x = 1;
	return eval(`(function () {
		eval("var x");
		return x;
	})`);
}

assert.sameValue(make()(), undefined);
