// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "A class declaration in a block shadows a variable of its name declared with the value of arguments"
---*/

function f() {
	var a = arguments;
	{
		class a {}
		return [a.length, a[0]].join();
	}
}
assert.sameValue(f(1, 2), "0,");
