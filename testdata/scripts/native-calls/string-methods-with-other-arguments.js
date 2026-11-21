// SPDX-License-Identifier: 0BSD
// SPDX-FileCopyrightText: 2026 Hajime Hoshi

/*---
description: "String methods called as methods convert receivers and arguments that are not strings and numbers within their calls"
---*/

// nativeLine returns the line of the stack trace that shows the native function that converted
// a throwing argument.
function nativeLine(f) {
	const thrower = {
		valueOf() {
			throw new Error("m");
		},
		toString() {
			throw new Error("m");
		},
	};
	try {
		f(thrower);
	} catch (e) {
		return e.stack.split("\n")[2];
	}
}
assert.sameValue(nativeLine(x => String.fromCharCode(x)), "    at String.fromCharCode (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".charAt(x)), "    at String.charAt (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".indexOf(x)), "    at String.indexOf (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".includes(x)), "    at String.includes (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".slice(x)), "    at String.slice (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".slice(0, x)), "    at String.slice (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".substring(x)), "    at String.substring (<anonymous>)");
assert.sameValue(nativeLine(x => "abc".substring(0, x)), "    at String.substring (<anonymous>)");

assert.sameValue(String.fromCharCode("66"), "B");
assert.sameValue(new String("xy").charAt(1), "y");
assert.sameValue("abc".charAt("2"), "c");
assert.sameValue("a1b".indexOf(1), 1);
assert.sameValue(new String("abc").includes("b"), true);
let caught;
try {
	"abc".includes(/a/);
} catch (e) {
	caught = e;
}
assert.sameValue(caught instanceof TypeError, true);
assert.sameValue(new String("hello").slice(1, 3), "el");
assert.sameValue("hello".slice("1"), "ello");
assert.sameValue(new String("hello").substring(3, 1), "el");
assert.sameValue("hello".substring(1, "3"), "el");
