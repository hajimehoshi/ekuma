// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a lexical binding hidden by a catch parameter blocks Annex B function hoisting from eval"
flags: [noStrict]
---*/

function test() {
	{
		let x;
		try {
			throw 0;
		} catch (x) {
			eval("{ function x() {} }");
		}
	}
	return typeof x;
}

assert.sameValue(test(), "undefined");
