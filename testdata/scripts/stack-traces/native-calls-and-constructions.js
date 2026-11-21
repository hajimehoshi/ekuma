// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a stack trace shows a running native function as a call or a construction"
---*/

function nativeLine(f) {
	try {
		f({
			valueOf() {
				throw new Error("m");
			},
		});
	} catch (e) {
		return e.stack.split("\n")[2];
	}
}

assert.sameValue(nativeLine(Number), "    at Number (<anonymous>)");
assert.sameValue(nativeLine(x => new Number(x)), "    at new Number (<anonymous>)");
