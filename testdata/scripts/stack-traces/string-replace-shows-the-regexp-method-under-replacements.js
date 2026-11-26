// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "a stack trace from a replacement function or object of String.prototype.replace shows RegExp.prototype[Symbol.replace]"
---*/

function below(f) {
	try {
		f();
	} catch (e) {
		return e.stack.split("\n").slice(2, 4).join("|");
	}
}

var want = "    at RegExp.[Symbol.replace] (<anonymous>)|    at String.replace (<anonymous>)";
assert.sameValue(below(() => "abc".replace(/b/, () => {
	throw new Error("m");
})), want);
assert.sameValue(below(() => "abc".replace(/b/, {
	toString() {
		throw new Error("m");
	},
})), want);
