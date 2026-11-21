// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "charCodeAt called as a method converts a receiver that is not a string and an index that is not a number"
---*/

const o = {
	charCodeAt: String.prototype.charCodeAt,
	toString() {
		return "xyz";
	},
};
assert.sameValue(o.charCodeAt(0), 120);
assert.sameValue(new String("abc").charCodeAt(2), 99);
assert.sameValue("abc".charCodeAt("1"), 98);
assert.sameValue("abc".charCodeAt({ valueOf: () => 2 }), 99);
assert.sameValue("abc".charCodeAt(1.9), 98);
assert.sameValue("abc".charCodeAt(-0.5), 97);
assert.sameValue("abc".charCodeAt(NaN), 97);
assert.sameValue("abc".charCodeAt(-1), NaN);
assert.sameValue("abc".charCodeAt(3), NaN);
assert.sameValue("abc".charCodeAt(Infinity), NaN);

// The conversion of the index runs inside the call of charCodeAt.
let line;
try {
	"abc".charCodeAt({
		valueOf() {
			throw new Error("m");
		},
	});
} catch (e) {
	line = e.stack.split("\n")[2];
}
assert.sameValue(line, "    at String.charCodeAt (<anonymous>)");
